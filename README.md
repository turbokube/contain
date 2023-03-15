
`contain` is a unix philosophy CLI that does the following thing well:
Produces a container image from a local directory structure and a base image.

It runs nicely with [Skaffold] as [custom](https://skaffold.dev/docs/builders/builder-types/custom/) `buildCommand`, as it picks up the `IMAGE` and `PLATFORMS` envs.
