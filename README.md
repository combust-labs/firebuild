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

## Create a dedicated CNI network for the builds:

Feel free to change the `ipam.subnet` of set multiple ones. `host-local` IPAM [CNI plugin documentation](https://www.cni.dev/plugins/ipam/host-local/).

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

## An example:

This will fail because the Dockerfile uses the ADD command. To succeed, clone the owning repository locally and reference the local file.

This example assumes that SSH agent is started and the relevant version SSH key is in the agent.

```sh
sudo /usr/local/go/bin/go run ./main.go build \
    --binary-firecracker=$(readlink /usr/bin/firecracker) \
    --binary-jailer=$(readlink /usr/bin/jailer) \
    --chroot-base=/srv/jailer \
    --dockerfile=https://raw.githubusercontent.com/hashicorp/docker-consul/master/0.X/Dockerfile \
    --machine-cni-network-name=machine-builds \
    --machine-rootfs-base=/firecracker/rootfs \
    --machine-ssh-user=alpine \
    --machine-vmlinux=/firecracker/vmlinux/vmlinux-v5.8
```

## Caveats when building from the URL

The `build` command will resolve the resources refernced in `ADD` and `COPY` commands even when loading the `Dockerfile` via the URL. The context root in this case will be established by removing the file name from the URL. An example:

- consider the URL `https://raw.githubusercontent.com/hashicorp/docker-consul/master/0.X/Dockerfile`
- the `Dockerfile` name will be removed from the URL and the context is `https://raw.githubusercontent.com/hashicorp/docker-consul/master/0.X`
- assuming that the `Dockerfile` contains `ADD ./docker-entrypoint.sh ...`, the resolver will try loading `https://raw.githubusercontent.com/hashicorp/docker-consul/master/0.X/docker-entrypoint.sh`

There are following limitations when loading the resources like that via URL:

- if the `ADD` or `COPY` points to a directory, the command will fail because there is no unified way of loading directories via HTTP, the resolver will not even attempt this, it will most likely fail on the `HTTP GET` request
- the file permissions will not be carried over because there is no method to infer file mode from a HTTP response

