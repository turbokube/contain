#!/usr/bin/env bash
[ -z "$DEBUG" ] || set -x
set -eo pipefail

SCRIPTPATH="$( cd -- "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"
export KUBECONFIG="$SCRIPTPATH/test/kubeconfig"

k() {
  kubectl $@
}

k3d cluster create turbokube-test-contain
until k get pods 2>/dev/null; do
  echo "==> Waiting for cluster to respond ..."
  sleep 1
done
until k get serviceaccount default 2>/dev/null; do
  echo "==> Waiting for the default service account to exist ..."
  sleep 1
done

echo "==> Applying test runtime"
k apply -k test/run-nodejs/

# in real use cases image should be configured to match runtime pod,
# but here we avoid the duplication to make it easier to test different images
image=$(yq e '.spec.template.spec.containers[0].image' test/run-nodejs/nodejs-watch-job.yaml)

# TODO unreachable cluster warrants retry: https://github.com/turbokube/contain/blob/v0.2.2/pkg/run/containersync.go#L95
sleep 1
kubectl get pods -o json > test/k8s-pods-json1.out
(cd test/run-nodejs/app/; contain -x -b $image -r turbokube.dev/contain-run=nodejs) || kubectl get pods -o json > test/k8s-pods-nodejs-json.out
# TODO contain could return a namespace+pod+container identifier upon successful sync

k3d cluster delete turbokube-test-contain
