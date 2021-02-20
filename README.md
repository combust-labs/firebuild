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