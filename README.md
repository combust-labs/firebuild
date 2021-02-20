# Build Firecracker VMMs from Dockerfiles

Work in progress.

## Build a base OS root file system

This process assumes that you have:

- a writable directory `/firecracker`
- a writable directory `$HOME/.ssh`
- `docker` without the need for `sudo`
- the user which executes the `build.sh` must be a `sudoer`

```sh
./baseos/_/apline/${version}/build.sh
```