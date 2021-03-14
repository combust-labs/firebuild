# Build Firecracker VMMs from Dockerfiles

A set of tools to build, run and operate Firecracker VMMs directly from `Dockerfiles`.

## Create a dedicated CNI network for the builds

Feel free to change the `ipam.subnet` or set multiple ones. `host-local` IPAM [CNI plugin documentation](https://www.cni.dev/plugins/ipam/host-local/).

```sh
cat <<EOF > /firecracker/cni/conf.d/machine-builds.conflist
{
    "name": "machine-builds",
    "cniVersion": "0.4.0",
    "plugins": [
        {
            "type": "bridge",
            "name": "builds-bridge",
            "bridge": "builds0",
            "isDefaultGateway": true,
            "ipMasq": true,
            "hairpinMode": true,
            "ipam": {
                "type": "host-local",
                "subnet": "192.168.128.0/24",
                "resolvConf": "/etc/resolv.conf"
            }
        },
        {
            "type": "firewall"
        },
        {
            "type": "tc-redirect-tap"
        }
    ]
}
EOF
```

## Build the base operating system root file system

Build a base operating system root file system. For example, Debian Buster slim:

```sh
sudo /usr/local/go/bin/go run ./main.go baseos \
    --dockerfile $(pwd)/baseos/_/debian/buster-slim/Dockerfile \
    --storage-provider=directory \
    --storage-provider.directory.rootfs-storage-root=/firecracker/rootfs \
    --tracing-enable
```

Because the `baseos` root file system is built completely with Docker, there is no need to configure the kernel storage.

It's possible to tag the baseos output using the `--tag=` argument, for example:

```sh
sudo /usr/local/go/bin/go run ./main.go baseos \
    --dockerfile $(pwd)/baseos/_/debian/buster-slim/Dockerfile \
    --storage-provider=directory \
    --storage-provider.directory.rootfs-storage-root=/firecracker/rootfs \
    --tag=custom/os:latest \
    --tracing-enable
```

### Why

TODO: explain why is the base operating system rootfs required.

## Create a Postgres 13 VMM rootfs from Debian Buster Dockerfile

```sh
sudo /usr/local/go/bin/go run ./main.go rootfs \
    --binary-firecracker=$(readlink /usr/bin/firecracker) \
    --binary-jailer=$(readlink /usr/bin/jailer) \
    --chroot-base=/srv/jailer \
    --dockerfile=git+https://github.com/docker-library/postgres.git:/13/Dockerfile \
    --storage-provider=directory \
    --storage-provider.directory.rootfs-storage-root=/firecracker/rootfs \
    --storage-provider.directory.kernel-storage-root=/firecracker/vmlinux \
    --machine-cni-network-name=machine-builds \
    --machine-ssh-user=debian \
    --machine-vmlinux-id=vmlinux-v5.8 \
    --pre-build-command='chmod 1777 /tmp' \
    --log-as-json \
    --resources-mem=512 \
    --tag=tests/postgres:13 \
    --tracing-enable
```

TODO: revisit the paragraph below

This service will not start automatically because the Postgres server requires an additional `export POSTGRES_PASSWORD=...` environment variable in the `/etc/firebuild/cmd.env` file. Right now, one needs to SSH to the VMM, `sudo echo 'export POSTGRES_PASSWORD' >> /etc/firebuild/cmd.env` and `sudo service DockerEntrypoint.sh start` to start the database.

## Run the VMM from the resulting tag

Once the root file system is built, start the VMM:

```sh
sudo /usr/local/go/bin/go run ./main.go run \
    --binary-firecracker=$(readlink /usr/bin/firecracker) \
    --binary-jailer=$(readlink /usr/bin/jailer) \
    --chroot-base=/srv/jailer \
    --storage-provider=directory \
    --storage-provider.directory.rootfs-storage-root=/firecracker/rootfs \
    --storage-provider.directory.kernel-storage-root=/firecracker/vmlinux \
    --from=tests/postgres:13 \
    --machine-cni-network-name=alpine \
    --machine-ssh-user=debian \
    --machine-vmlinux-id=vmlinux-v5.8 \
    --tracing-enable
```

### Additional settings

- `--daemonize`: when specified, runs the VMM in a daemonized mode
- `--env-file`: full path to the environment file, multiple OK
- `--env`: environment variable to deploy to configure the VMM with, multiple OK, format `--env=VAR_NAME=value`
- `--hostname`: hostname to apply to the VMM
- `--identity-file`: full path to the publish SSH key to deploy to the running VMM
- `--disable-pseudo-cloud-init`: disables inecting environment variables, hostname and identity files into the VMM

### Environment merging

The final environment variables are written to `/etc/profile.d/run-env.sh` file. All file specified with `--env-file` are merged first in the order of occurrcence, variables specified with `--env` are merged last.

## Terminating a daemonized VMM

A VMM started with the `--daemonize` flag can be stopped in three ways:

- by executing the `kill` tool command, this is a clean stop which will take care of all the necessry clean up
- by executing `reboot` from inside of the VMM SSH connection; unclean stop, manual purge of the CNI cache, jailer directory, run cache and the veth link is needed
- by executing the cURL HTTP against the VMM socket file; unclean stop, manual purge of the CNI cache, jailer directory, run cache and the veth link is needed

### VMM kill command

To get the VMM ID, look closely at the output of the `run ... --detached` command:

```json
{
    "@level":"info",
    "@message":"VMM running as a daemon",
    "@module":"run",
    "@timestamp":"2021-03-09T19:55:41.684488Z",
    "cache-dir":"/var/lib/firebuild/831b7068f7924584b384260e8d262834",
    "ip-address":"192.168.127.3",
    "ip-net":"192.168.127.3/24",
    "jailer-dir":"/srv/jailer/firecracker-v0.22.4-x86_64/831b7068f7924584b384260e8d262834",
    "pid":17904,
    "veth-name":"vethydMSApKfoDu",
    "vmm-id":"831b7068f7924584b384260e8d262834"
}
```

Copy the VMM ID from the output and run:

```sh
sudo /usr/local/go/bin/go run ./main.go kill --vmm-id=${VMMID}
```

### Purging remains of the VMMs stopped without the kill command

If a VMM exits in any other way than via `kill` command, following data continues residing on the host:

- jail directory with all contents
- run cache directory with all contents
- CNI interface with CNI cache directory

To remove this data, run the `purge` command. 

```sh
sudo /usr/local/go/bin/go run ./main.go purge
```

### List VMMs

```sh
sudo /usr/local/go/bin/go run ./main.go ls
```

Example output:

```
2021-03-12T01:46:21.752Z [INFO]  ls: vmm: id=df45b6e14538456286e4a4bc1f9bf6e2 running=true pid=20658 image=tests/postgres:13 started="2021-03-12 01:46:11 +0000 UTC" ip-address=192.168.127.9
```

## Dockerfile git+http(s):// URL

It's possible to reference a `Dockerfile` residing in the git repository available under a HTTP(s) URL. Here's an example:

```sh
sudo /usr/local/go/bin/go run ./main.go rootfs \
    --binary-firecracker=$(readlink /usr/bin/firecracker) \
    --binary-jailer=$(readlink /usr/bin/jailer) \
    --chroot-base=/srv/jailer \
    --dockerfile=git+https://github.com/hashicorp/docker-consul.git:/0.X/Dockerfile#master \
    --storage-provider=directory \
    --storage-provider.directory.rootfs-storage-root=/firecracker/rootfs \
    --storage-provider.directory.kernel-storage-root=/firecracker/vmlinux \
    --machine-cni-network-name=machine-builds \
    --machine-ssh-user=alpine \
    --machine-vmlinux-id=vmlinux-v5.8 \
    --post-build-command='chmod -x /etc/init.d/sshd' \
    --pre-build-command='rm -rf /var/cache/apk && mkdir -p /var/cache/apk && sudo apk update' \
    --log-as-json \
    --tag=tests/consul:1.9.3 \
    --service-file-installer=$(pwd)/baseos/_/alpine/alpine.local.d.service.sh \
    --tracing-enable
```

The URL format is:

```
git+http(s)://host:port/path/to/repo.git:/path/to/Dockerfile[#<commit-hash | branch-name | tag-name>]
```

And will be processed as:

- path `/path/to/repo.git:/path/to/Dockerfile` will be split by `:` and must contain both sides
  - `/path/to/repo.git` is the git repository path
  - `/path/to/Dockerfile` is the path to the `Dockerfile` in the repository, must point to a file after clone and checkout
- optional `#fragment` may be a comit hash, a branch name or a tag name
  - if no `#fragment` is given, the program will use the default cloned branch, check the remote to find out what is it
- the cloned repository will have a single remote and the first remote wil be used

Git repositories support file modes and the files from the `ADD` and `COPY` directives, will have file info modes applied. Example:

```json
{
    "@level":"debug",
    "@message":"Running remote command",
    "@module":"build.remote-client.connected-remote-client",
    "@timestamp":"2021-02-21T13:46:49.152941Z",
    "command":"sudo mkdir -p / \u0026\u0026 sudo /bin/sh -c 'chmod 0755 /tmp/QdWsoItNoHEOgarVrHGHzWBrEzDhpHMw/TfRemDQEtpRtGKbDEjlPcJvNGDOxJSNu'",
    "ip-address":"192.168.128.56",
    "veth-name":"vethkBkhSAovlpr",
    "vmm-id":"2ad5e481be144d3da7181abf124e23cf"
}
```

## Supported Dockerfile URL formats

- `http://` and `https://` for direct paths to the `Dockerfile`, these can handle single file only and do not attempt loading any resources handled by `ADD` / `COPY` commands, the server must be capable of responding to `HEAD` and `GET` http requests, more details in `Caveats when building from the URL` further in this document
- special `git+http://` and `git+https://`, documented above
- standard `ssh://`, `git://` and `git+ssh://` URL formats with the expectation that the path meets the criteria from the `git+http(s):// URL` section above

## Caveats when building from the URL

The `build` command will resolve the resources referenced in `ADD` and `COPY` commands even when loading the `Dockerfile` via the URL. The context root in this case will be established by removing the file name from the URL. An example:

- consider the URL `https://raw.githubusercontent.com/hashicorp/docker-consul/master/0.X/Dockerfile`
- the `Dockerfile` name will be removed from the URL and the context is `https://raw.githubusercontent.com/hashicorp/docker-consul/master/0.X`
- assuming that the `Dockerfile` contains `ADD ./docker-entrypoint.sh ...`, the resolver will try loading `https://raw.githubusercontent.com/hashicorp/docker-consul/master/0.X/docker-entrypoint.sh`

There are following limitations when loading the resources like that via URL:

- if the `ADD` or `COPY` points to a directory, the command will fail because there is no unified way of loading directories via HTTP, the resolver will not even attempt this, it will most likely fail on the `HTTP GET` request
- the file permissions will not be carried over because there is no method to infer file mode from a HTTP response

## Unsupported Dockerfile features

The build program does not support:

- `ONBUILD` commands
- `HEALTHCHECK` commands
- `STOPSIGNAL` commands

## Multi-stage Dockerfile builds

The program intends to support multi-stage `Dockerfile` builds. An example with [grepplabs Kafka Proxy](https://github.com/grepplabs/kafka-proxy).

Build v0.2.8 using git repository link, leave SSH access on:

```sh
/usr/local/go/bin/go run ./main.go rootfs \
    --binary-firecracker=$(readlink /usr/bin/firecracker) \
    --binary-jailer=$(readlink /usr/bin/jailer) \
    --chroot-base=/srv/jailer \
    --dockerfile=git+https://github.com/grepplabs/kafka-proxy.git:/Dockerfile#v0.2.8 \
    --storage-provider=directory \
    --storage-provider.directory.rootfs-storage-root=/firecracker/rootfs \
    --storage-provider.directory.kernel-storage-root=/firecracker/vmlinux \
    --machine-cni-network-name=machine-builds \
    --machine-ssh-user=alpine \
    --machine-vmlinux-id=vmlinux-v5.8 \
    --pre-build-command='rm -rf /var/cache/apk && mkdir -p /var/cache/apk && sudo apk update' \
    --log-as-json \
    --tag=tests/kafka-proxy:0.2.8 \
    --service-file-installer=$(pwd)/baseos/_/alpine/alpine.local.d.service.sh \
    --tracing-enable
```

## Tracing

Start Jaeger, for example:

```sh
docker run --rm -ti \
    -e COLLECTOR_ZIPKIN_HTTP_PORT=9411 \
    -p 5775:5775/udp \
    -p 6831:6831/udp \
    -p 6832:6832/udp \
    -p 5778:5778 \
    -p 16686:16686 \
    -p 14268:14268 \
    -p 14250:14250 \
    -p 9411:9411 \
    jaegertracing/all-in-one:1.22
```

And configure respective commands with:

```sh
... --tracing-enable \
--tracing-collector-host-port=... \
```

The default value of the `--tracing-collector-host-port` is `127.0.0.1:6831`. To enable tracer log output, set `--tracing-log-enable` flag.

## License

Unless explcitly stated: AGPL-3.0 License.

Excluded from the license:

- `build/env/expand.go`: sourced from golang standard library
- `remote/scp.go`: sourced from Terraform SSH communicator
