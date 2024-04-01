#!/usr/bin/env bash
[ -z "$DEBUG" ] || set -x
set -eo pipefail
YBIN="$(dirname $0)"

GITADD="test/baseregistry"
[ -n "$ROOT" ] || ROOT="$(pwd)/$GITADD"
REGISTRYNAME=testregistry
BUILDERNAME=containbuild

[ -z "$(ls -A "$ROOT")" ] || (echo "-> Non-empty rootdirectory $ROOT" && exit 1)

echo "-> Create base registry ..."
docker stop $REGISTRYNAME 2>/dev/null || true
docker rm $REGISTRYNAME 2>/dev/null || true
docker run --name $REGISTRYNAME \
  -u $(id -u):$(id -g) \
  -p 5000 \
  -v "$ROOT:/var/lib/registry" \
  -e REGISTRY_STORAGE=filesystem \
  -e REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY=/var/lib/registry \
  -e REGISTRY_LOG_LEVEL=debug \
  --rm -d \
  registry:2

HOSTPORT=$(docker inspect $REGISTRYNAME -f '{{ (index (index .NetworkSettings.Ports "5000/tcp") 0).HostPort }}')
HOSTADDR=host.docker.internal

echo "-> Check registry endpoint ..."
[ -z "$DEBUG" ] || docker run --rm curlimages/curl:8.6.0 -v http://$HOSTADDR:$HOSTPORT/v2/
REGISTRY_IP=$(docker run --rm curlimages/curl:8.6.0 -f http://$HOSTADDR:$HOSTPORT/v2/ -o /dev/null -w '%{remote_ip}\n')

# ended up using IP, named registry hosts remain here but seemed to fail on name resolution
BUILDKIT_CONFIG=$(mktemp)
cat <<EOF | tee $BUILDKIT_CONFIG
debug = true
[registry."$REGISTRYNAME"]
  mirrors = ["$REGISTRY_IP:$HOSTPORT"]
  http = true
[registry."$REGISTRYNAME.local"]
  mirrors = ["$REGISTRY_IP:$HOSTPORT"]
  http = true
[registry."$REGISTRY_IP:$HOSTPORT"]
  http = true
EOF

echo "-> Configure builder ..."
docker buildx stop containbuild 2>/dev/null || true
docker buildx rm containbuild 2>/dev/null || true
docker buildx create --name $BUILDERNAME \
  --config $BUILDKIT_CONFIG \
  --driver-opt image=moby/buildkit:v0.13.1 \
  --bootstrap

# https://www.docker.com/blog/highlights-buildkit-v0-11-release/#3-source-date-epoch
export SOURCE_DATE_EPOCH=0

for B in $(find . -type d -name baseimage-* | sed 's|^\./test/||'); do \
  echo "-> Build $B (with sbom and provenance - i.e. not reproducible and thus not included in git added baseregistry) ..."
  docker buildx build \
    --builder $BUILDERNAME \
    --platform=linux/amd64,linux/arm64/v8 \
    --output type=image,push=true,name=$REGISTRY_IP:$HOSTPORT/contain-test/$B:latest,oci-mediatypes=true,rewrite-timestamp=true \
    ./test/$B
  crane manifest localhost:$HOSTPORT/contain-test/$B:latest | grep mediaType
  echo "-> Build $B without attestation (docker manifest type) ..."
  docker buildx build \
    --builder $BUILDERNAME \
    --platform=linux/amd64,linux/arm64/v8 \
    --sbom=false --provenance=false \
    --output type=image,push=true,name=$REGISTRY_IP:$HOSTPORT/contain-test/$B:noattest-docker,oci-mediatypes=false,rewrite-timestamp=true \
    ./test/$B
  crane manifest localhost:$HOSTPORT/contain-test/$B:noattest-docker | grep mediaType
  echo "-> Build $B without attestation (OCI manifest type) ..."
  docker buildx build \
    --builder $BUILDERNAME \
    --platform=linux/amd64,linux/arm64/v8 \
    --sbom=false --provenance=false \
    --output type=image,push=true,name=$REGISTRY_IP:$HOSTPORT/contain-test/$B:noattest,oci-mediatypes=true,rewrite-timestamp=true \
    ./test/$B
  crane manifest localhost:$HOSTPORT/contain-test/$B:noattest | grep mediaType
done

echo "-> Cleanup ..."
docker buildx stop --builder $BUILDERNAME
docker buildx rm --builder $BUILDERNAME

echo "-> Note that $GITADD requires git add -f (it's in .gitignore so test pushes stay unversioned)"
