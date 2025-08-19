# contain

Simple declarative container builds from local artifacts.

`contain` is a unix philosophy CLI that does the following thing well:
Produces a container image from a local directory structure and a base image.

It runs nicely with [Skaffold](https://skaffold.dev/) as [custom](https://skaffold.dev/docs/builders/builder-types/custom/) `buildCommand`, as it picks up the `IMAGE` and `PLATFORMS` envs.

## commands

The CLI now has sub-commands (migrated to [cobra](https://github.com/spf13/cobra)):

- `contain build` – existing build/append functionality (also the default when you invoke just `contain ...` for backwards compatibility).
- `contain sbom` – experimental stub that will generate a Software Bill of Materials from a build metadata file in future versions. Currently it just echoes the provided `--build-metadata` path.

Examples (old style still works):

```
contain -x -b busybox:latest --file-output out.json
contain build -x -b busybox:latest --file-output out.json
contain sbom --build-metadata out/localdir.buildctl.json
```


## basics

Contain is designed to take platform-agnostic layers and append to multi-platform bases.
Nodejs and Java are examples of runtime environments that work well with such images.

Future versions might add support for:
- Single platform base images (can be auto detected)
- Configuring platform per layer, i.e. omit a layer on non-matching platforms

To leave room for single platform images, Contain requires that you set platforms to `all`,
the same value you'd use for [ko](https://github.com/ko-build/ko/) multi-platform images.

There are many image manifests formats but Contain supports only OCI.
By validating manifest types Contain helps keeping your images consistent.
Future versions could add support for other formats, but that would be opt-in by config.

### execution

Here's an example of a base image manifest, with optional attestation:

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.index.v1+json",
  "manifests": [
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:2e2643ac2745067b637a4e1c4d5a3936b27a430cf0d989562c04fb7d7c53e69c",
      "size": 475,
      "platform": {
        "architecture": "amd64",
        "os": "linux"
      }
    },
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:e34c5ca17d295d5873f451ab094fb5b5515a0a5ec433d8613276baeb8f1c7741",
      "size": 475,
      "platform": {
        "architecture": "arm64",
        "os": "linux"
      }
    },
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:897bf5e232d9c5a72947462cc13072e988428e0ff80f4441c7a238e4892afc00",
      "size": 566,
      "annotations": {
        "vnd.docker.reference.digest": "sha256:2e2643ac2745067b637a4e1c4d5a3936b27a430cf0d989562c04fb7d7c53e69c",
        "vnd.docker.reference.type": "attestation-manifest"
      },
      "platform": {
        "architecture": "unknown",
        "os": "unknown"
      }
    },
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:a7f5930278c418d53dc56bfcd22f7332fbda225006a1875fbc673df454929a49",
      "size": 566,
      "annotations": {
        "vnd.docker.reference.digest": "sha256:e34c5ca17d295d5873f451ab094fb5b5515a0a5ec433d8613276baeb8f1c7741",
        "vnd.docker.reference.type": "attestation-manifest"
      },
      "platform": {
        "architecture": "unknown",
        "os": "unknown"
      }
    }
  ]
}
```

This means that to get the actual base layer references, Contain will have to pull both of the `application/vnd.oci.image.manifest.v1+json` manifests. They're also pretty-printed json, like

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "config": {
    "mediaType": "application/vnd.oci.image.config.v1+json",
    "digest": "sha256:0d0715737c21d4dc2a49af26ef780241ad5d6ab1a0e1133364e40d002ca16722",
    "size": 575
  },
  "layers": [
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "digest": "sha256:c61587a79a418fb6188de8add2e9f694b012acde27abefd27dedaff5f02de71e",
      "size": 93
    }
  ]
}
```

Running `sha256sum` on the above you get `2e2643ac2745067b637a4e1c4d5a3936b27a430cf0d989562c04fb7d7c53e69c` which is the digest in the index manifest.
Using this manifest you can retrieve the layers,
but because Contain is agnostic to what the base image contains there's no need to spend the bandwidth of pulling them.

Later on if the resulting image is pushed to a different registry than where the base image lives,
[go-containerregistry](https://github.com/google/go-containerregistry) will handle the copying of all layers.

Contain does not support nested indexes. It will bail if any manifest in the index has mediaType `application/vnd.oci.image.index.v1+json`.

Upon successful retrieval of indexes, contain can start the actual build.

Layers are just tarballs. The task for Contain is to produce these `tar+gzip`s from your config,
hash them and append _each layer_ in order to _each platform_'s index.
That creates a new index per platform, each one having a new digest (checksum).
With those checksums Contain can produce a new index.

In practice Contain will run append and push to the first platform,
then derive the layers to append from that one.

Currently Contain can't update attestations. Those index entries are therefore dropped.

## Configuration

Contain supports template variables in config yaml using the framework from [Skaffold](https://skaffold.dev/docs/environment/templating/).

## Reproducible Builds

Contain implements reproducible builds using deterministic layer creation:

- **Timestamps**: All files and directories in layers use `SOURCE_DATE_EPOCH` (1970-01-01T00:00:00Z) for reproducible timestamps
- **File Modes**: File permissions are normalized to 0644 for regular files and 0755 for directories by default
- **Executable Preservation**: The executable bit (0111) is preserved from source files when present
- **Mode Override**: Layer attributes can override the default file and directory modes
- **Symlink Support**: Symbolic links pointing within the source tree are preserved with their target paths
- **Directory Inclusion**: Directory entries are explicitly included in layers for complete filesystem representation

### Mode Configuration

You can override the default file and directory modes using layer attributes:

```yaml
layers:
  - localDir:
      path: ./build
      containerPath: /app
    layerAttributes:
      mode: 0600        # Override file mode
      dirMode: 0700     # Override directory mode
      uid: 1000
      gid: 1000
```

### Reproducible Layer Content

The reproducible build implementation ensures that:

1. **Identical source** produces **identical layers** regardless of build time or environment
2. **File metadata** is normalized to prevent variations from filesystem differences
3. **Directory structure** is completely captured including empty directories
4. **Symlinks** are preserved when they point within the source tree
5. **Build timestamps** do not affect layer checksums

This enables reliable caching, content-addressable storage, and deterministic container image builds.

## Migration to Reproducible Builds

This version introduces major changes for reproducible builds that affect layer digests:

- **Breaking Change**: All layer digests will change due to normalized timestamps and file modes
- **Test Updates Required**: ExpectDigest values in integration tests need updating
- **Backward Compatibility**: Configuration remains compatible, only layer content changes
- **Benefits**: Builds are now deterministic across environments and time

To update existing configurations, simply rebuild your images - the functionality remains the same but with improved reproducibility guarantees.
