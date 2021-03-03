# Build Firecracker VMMs from Dockerfiles

Work in progress.

## Build a base OS root file system

This process assumes that you have:

- a writable directory `/firecracker`
- a writable directory `$HOME/.ssh`
- `docker` without the need for `sudo`
- the user which executes the `build.sh` must be a `sudoer`

```sh
./baseos/_/alpine/${version}/build.sh
```

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

## An example

This will fail because the Dockerfile uses the `ADD` command. To succeed, clone the owning repository locally and reference the local file. Eventually, use the `git+http(s)://` URL scheme.

This example assumes that SSH agent is started and the relevant version SSH key is in the agent.

```sh
sudo bash
/usr/local/go/bin/go run ./main.go build \
    --binary-firecracker=$(readlink /usr/bin/firecracker) \
    --binary-jailer=$(readlink /usr/bin/jailer) \
    --chroot-base=/srv/jailer \
    --dockerfile=https://raw.githubusercontent.com/hashicorp/docker-consul/master/0.X/Dockerfile \
    --machine-cni-network-name=machine-builds \
    --machine-rootfs-base=/firecracker/rootfs \
    --machine-ssh-user=alpine \
    --machine-vmlinux=/firecracker/vmlinux/vmlinux-v5.8 \
    --init-command='rm -rf /var/cache/apk && mkdir -p /var/cache/apk && sudo apk update' \
    --tag=tests/consul:1.9.3
```

## git+http(s):// URL

It's possible to reference a `Dockerfile` residing in the git repository available under a HTTP(s) URL. Here's an example:

```sh
/usr/local/go/bin/go run ./main.go build \
    --binary-firecracker=$(readlink /usr/bin/firecracker) \
    --binary-jailer=$(readlink /usr/bin/jailer) \
    --chroot-base=/srv/jailer \
    --dockerfile=git+https://github.com/hashicorp/docker-consul.git:/0.X/Dockerfile#master \
    --machine-cni-network-name=machine-builds \
    --machine-rootfs-base=/firecracker/rootfs \
    --machine-ssh-user=alpine \
    --machine-vmlinux=/firecracker/vmlinux/vmlinux-v5.8 \
    --post-build-command='chmod -x /etc/init.d/sshd' \
    --pre-build-command='rm -rf /var/cache/apk && mkdir -p /var/cache/apk && sudo apk update' \
    --log-as-json \
    --tag=tests/consul:1.9.3 \
    --service-file-installer=$(pwd)/baseos/_/alpine/alpine.local.d.service.sh
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

## Supported URL formats

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
/usr/local/go/bin/go run ./main.go build \
    --binary-firecracker=$(readlink /usr/bin/firecracker) \
    --binary-jailer=$(readlink /usr/bin/jailer) \
    --chroot-base=/srv/jailer \
    --dockerfile=git+https://github.com/grepplabs/kafka-proxy.git:/Dockerfile#v0.2.8 \
    --machine-cni-network-name=machine-builds \
    --machine-rootfs-base=/firecracker/rootfs \
    --machine-ssh-user=alpine \
    --machine-vmlinux=/firecracker/vmlinux/vmlinux-v5.8 \
    --pre-build-command='rm -rf /var/cache/apk && mkdir -p /var/cache/apk && sudo apk update' \
    --log-as-json \
    --tag=tests/kafka-proxy:0.2.8 \
    --service-file-installer=$(pwd)/baseos/_/alpine/alpine.local.d.service.sh
```

## License

Unless explcitly stated: AGPL-3.0 License.

Excluded from the license:

- `build/env/expand.go`: sourced from golang standard library
- `remote/scp.go`: sourced from Terraform SSH communicator