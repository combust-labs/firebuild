# Convenience of containers, security of virtual machines

With firebuild, you can build and deploy secure VMs directly from `Dockerfiles` and Docker images in just few minutes.

The concept of `firebuild` is to leverage as much of the existing Docker world as possible. There are thousands of Docker images out there. Docker images are awesome because they encapsulate the software we want to run in our workloads, they also encapsulate dependencies. Dockerfiles are what Docker images are built from. Dockeriles are the blueprints of the modern infrastructure. There are thousands of them for almost anything one can imagine and new ones are very easy to write.

With firebuild it is possible to:

- build root file systems directly from Dockerfiles
- tag and version root file systems
- run and manage microvms on a single host
- define run profiles

## High level example

Build and start HashiCorp Consul 1.9.4 on Firecracker with three simple steps:

- build a base operating system image
- build Consul image
- start the application

```sh
sudo $GOPATH/bin/firebuild baseos \
    --profile=standard \
    --dockerfile $(pwd)/baseos/_/alpine/3.12/Dockerfile
```

```sh
sudo $GOPATH/bin/firebuild rootfs \
    --profile=standard \
    --dockerfile=git+https://github.com/hashicorp/docker-consul.git:/0.X/Dockerfile \
    --cni-network-name=machine-builds \
    --ssh-user=alpine \
    --vmlinux-id=vmlinux-v5.8 \
    --tag=combust-labs/consul:1.9.4
```

```sh
sudo $GOPATH/bin/firebuild run \
    --profile=standard \
    --name=consul1 \
    --from=combust-labs/consul:1.9.4 \
    --cni-network-name=machines \
    --vmlinux-id=vmlinux-v5.8
```

Find the IP of the `consul1` VM and query Consul:

```sh
VMIP=$(sudo $GOPATH/bin/firebuild inspect \
    --profile=standard \
    --vmm-id=consul1 | jq '.NetworkInterfaces[0].StaticConfiguration.IPConfiguration.IP' -r)
```

```sh
$ curl http://${VMIP}:8500/v1/status/leader
"127.0.0.1:8300"
```

## But how?

### clone and build from sources

```sh
mkdir -p $GOPATH/src/github.com/combust-labs/firebuild
cd $GOPATH/src/github.com/combust-labs/firebuild
go install
```

The binary will be placed in `$GOPATH/bin/firebuild`.

### create a profile

```sh
# create required directories, these need to exist before the profile can be created:
sudo mkdir -p /firecracker/rootfs
sudo mkdir -p /firecracker/vmlinux
sudo mkdir -p /srv/jailer
sudo mkdir -p /var/lib/firebuild
# create a profile:
sudo $GOPATH/bin/firebuild profile-create \
	--profile=standard \
	--binary-firecracker=$(readlink /usr/bin/firecracker) \
	--binary-jailer=$(readlink /usr/bin/jailer) \
	--chroot-base=/srv/jailer \
	--run-cache=/var/lib/firebuild \
	--storage-provider=directory \
	--storage-provider-property-string="rootfs-storage-root=/firecracker/rootfs" \
	--storage-provider-property-string="kernel-storage-root=/firecracker/vmlinux" \
	--tracing-enable
```

Kernel images will be stored in `/firecracker/vmlinux`, root file systems will be stored in `/firecracker/rootfs`.

### build the kernel

The examples use the 5.8 Linux kernel image which is built using the configuration from the `baseos/kernel/5.8.config` file in this repository. To build the kernel:

```sh
export KERNEL_VERSION=v5.8
mkdir -p /tmp/linux && cd /tmp/linux
git clone https://github.com/torvalds/linux.git .
git checkout ${KERNEL_VERSION}
wget -O .config https://raw.githubusercontent.com/combust-labs/firebuild/master/baseos/kernel/5.8.config
make vmlinux -j32 # adapt to the number of cores you have
```

Once built, copy the kernel to the storage:

```sh
mv /tmp/linux/vmlinux /firecracker/vmlinux/vmlinux-${KERNEL_VERSION}
```

### setup CNI

`firebuild` assumes CNI availability. Installing the plugins is very straightforward. Create `/opt/cni/bin/` directory and download the plugins:

```sh
mkdir -p /opt/cni/bin
curl -O -L https://github.com/containernetworking/plugins/releases/download/v0.9.1/cni-plugins-linux-amd64-v0.9.1.tgz
tar -C /opt/cni/bin -xzf cni-plugins-linux-amd64-v0.9.1.tgz
```

Firecracker also requires the `tc-redirect-tap` plugin. Unfortunately, this one does not offer downloadable binaries and has to be built from sources.

```sh
mkdir -p $GOPATH/src/github.com/awslabs/tc-redirect-tap
cd $GOPATH/src/github.com/awslabs/tc-redirect-tap
git clone https://github.com/awslabs/tc-redirect-tap.git .
make install
```

### create a dedicated CNI network for the builds

Feel free to change the `ipam.subnet` or set multiple ones. `host-local` IPAM [CNI plugin documentation](https://www.cni.dev/plugins/ipam/host-local/).

```sh
cat <<EOF > /etc/cni/conf.d/machine-builds.conflist
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

#### caution

The [maximum socket path in the Linux Kernel](https://github.com/torvalds/linux/blob/master/include/uapi/linux/un.h) is `107` characters + `\0`:

```c
struct sockaddr_un {
	__kernel_sa_family_t sun_family; /* AF_UNIX */
	char sun_path[UNIX_PATH_MAX];	/* pathname */
};
```

The `--chroot-base` value must have a maximum length of `31` characters. The constant jailer path suffix used by `firebuild` is `76` characters:

- constant `/firecracker-v0.22.4-x86_64/` (automatically generated by the jailer)
- VM ID is always `20` characters long
- constant `/root/run/firecracker.socket` assumed by the jailer

Example: `/firecracker-v0.22.4-x86_64/sifuqm4rq2runxparjcx/root/run/firecracker.socket`.

**Using more than `31` characters for the `--chroot-base` value, regardless if in the profile setting or using the command `--chroot-base` flag, will lead to a very obscure error**. Firecracker will report an error similar to:

```
INFO[0006] Called startVMM(), setting up a VMM on /mnt/sdd1/firebuild/jailer/firecracker-v0.22.4-x86_64/6b41ecc3783c4f38a743c9c8af4bbe0f/root/run/firecracker.socket
WARN[0009] Failed handler "fcinit.StartVMM": Firecracker did not create API socket /mnt/sdd1/firebuild/jailer/firecracker-v0.22.4-x86_64/6b41ecc3783c4f38a743c9c8af4bbe0f/root/run/firecracker.socket: context deadline exceeded
{"@level":"error","@message":"Firecracker VMM did not start, build failed","@module":"rootfs","@timestamp":"2021-03-14T19:20:49.856228Z","reason":"Failed to start machine: Firecracker did not create API socket /mnt/sdd1/firebuild/jailer/firecracker-v0.22.4-x86_64/6b41ecc3783c4f38a743c9c8af4bbe0f/root/run/firecracker.socket: context deadline exceeded","veth-name":"vethHvfZiskhLkQ","vmm-id":"6b41ecc3783c4f38a743c9c8af4bbe0f"}
{"@level":"info","@message":"cleaning up jail directory","@module":"rootfs","@timestamp":"2021-03-14T19:20:49.856407Z","veth-name":"vethHvfZiskhLkQ","vmm-id":"6b41ecc3783c4f38a743c9c8af4bbe0f"}
{"@level":"info","@message":"cleaning up temp build directory","@module":"rootfs","@timestamp":"2021-03-14T19:20:49.856458Z"}
WARN[0010] firecracker exited: signal: killed
```

In the above example, the path is `114` characters long. Changing the chroot to `/mnt/sdd1/fc/jail` would solve the problem.

### build the base operating system root file system

`firebuild` uses the Docker metaphor. An image of an application is built `FROM` a base. An application image can be built `FROM alpine:3.13`, for example. Or `FROM debian:buster-slim`, or `FROM registry.access.redhat.com/ubi8/ubi-minimal:8.3` and dozens others.

In order to fulfill those semantics, a base operating system image must be built before the application root file system can be created.

Build a base Debian Buster slim:

```sh
sudo $GOPATH/bin/firebuild baseos \
    --profile=standard \
    --dockerfile $(pwd)/baseos/_/debian/buster-slim/Dockerfile
```

Because the `baseos` root file system is built completely with Docker, there is no need to configure the kernel storage.

**This does not belong here, structure better**: It's possible to tag the `baseos` output using the `--tag=` argument, for example:

```sh
sudo $GOPATH/bin/firebuild baseos \
    --profile=standard \
    --dockerfile $(pwd)/baseos/_/debian/buster-slim/Dockerfile \
    --tag=custom/os:latest
```

### create a Postgres 13 VM rootfs directly from the upstream Dockerfile

The upstream `Dockerfile` is built `FROM debian:buster-slim`, that's the `baseos` built in the previous step:

```sh
sudo $GOPATH/bin/firebuild rootfs \
    --profile=standard \
    --dockerfile=git+https://github.com/docker-library/postgres.git:/13/Dockerfile \
    --cni-network-name=machine-builds \
    --vmlinux-id=vmlinux-v5.8 \
    --mem=512 \
    --tag=combust-labs/postgres:13
```

### create a separate CNI network for running VMs

For example:

```sh
cat <<EOF > /etc/cni/conf.d/machines.conflist
{
    "name": "machines",
    "cniVersion": "0.4.0",
    "plugins": [
        {
            "type": "bridge",
            "name": "machines-bridge",
            "bridge": "machines0",
            "isDefaultGateway": true,
            "ipMasq": true,
            "hairpinMode": true,
            "ipam": {
                "type": "host-local",
                "subnet": "192.168.127.0/24",
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

### run the VM from the resulting tag

Once the root file system is built, start the VM:

```sh
sudo $GOPATH/bin/firebuild run \
    --profile=standard \
    --name=postgres1 \
    --from=combust-labs/postgres:13 \
    --cni-network-name=machines \
    --vmlinux-id=vmlinux-v5.8 \
    --mem=512 \
    --env="POSTGRES_PASSWORD=some-password"
```

To avoid passing the password on the command line, you can use `--env-file` flag instead. The database is running, to verify:

Fine the IP address of the Postgres VM:

```sh
VMIP=$(sudo $GOPATH/bin/firebuild inspect \
    --profile=standard \
    --vmm-id=postgres1 | jq '.NetworkInterfaces[0].StaticConfiguration.IPConfiguration.IP' -r)
```

```sh
$ nc -zv ${VMIP} 5432
Connection to 192.168.127.94 5432 port [tcp/postgresql] succeeded!
```

If SSH access to the VM is required, this command can be used instead:

```sh
sudo $GOPATH/bin/firebuild run \
    --profile=standard \
    --name=postgres2 \
    --from=combust-labs/postgres:13 \
    --cni-network-name=machines \
    --vmlinux-id=vmlinux-v5.8 \
    --mem=512 \
    --env="POSTGRES_PASSWORD=some-password" \
    --ssh-user=debian \
    --identity-file=path/to/the/identity.pub
```

#### additional run flags

- `--daemonize`: when specified, runs the VM in a daemonized mode
- `--env-file`: full path to the environment file, multiple OK
- `--env`: environment variable to deploy to configure the VM with, multiple OK, format `--env=VAR_NAME=value`
- `--hostname`: hostname to apply to the VM which the VM uses to resolve itself
- `--name`: name of the virtual machine, if empty, random string will be used, maxmimum 20 characters, only `a-zA-Z0-9` ranges are allowed
- `--ssh-user`: username to get access to the VM via SSH with, these are defined in the `baseos` Dockerfiles and follow the EC2 pattern: `alpine` for Alpine images and `debian` for Debian image; together with `--identity-file` allows access to the running VM via SSH
- `--identity-file`: full path to the publish SSH key to deploy to the running VM

#### environment merging

The final environment variables are written to `/etc/profile.d/run-env.sh` file. All files specified with `--env-file` are merged first in the order of occurrcence, variables specified with `--env` are merged last.

### build directly from a Docker image

Sometimes having just the `Dockerfile` is not sufficient to execute a `rootfs` build. A good example is this [Jaeger all-in-one `Dockerfile`](https://github.com/jaegertracing/jaeger/blob/master/cmd/all-in-one/Dockerfile). The `Dockerfile` depends on the binary artifact built via `Makefile` prior to Docker build. In this case, it's possible to build the VM rootfs directly from the Docker image:

```sh
sudo $GOPATH/bin/firebuild rootfs \
    --profile=standard \
    --docker-image=jaegertracing/all-in-one:1.22 \
    --docker-image-base=alpine:3.13 \
    --cni-network-name=machine-builds \
    --vmlinux-id=vmlinux-v5.8 \
    --mem=512 \
    --tag=combust-labs/jaeger-all-in-one:1.22
```

The `--docker-image-base` is required because the underlying operating system the image was built from cannot be established from the Docker manifest.

To access the Jaeger Query UI via the host:

```
sudo iptables -t filter -A FORWARD \
    -m comment --comment "jaeger:1.22" \
    -p tcp -d 192.168.127.100 --dport 16686 \
    -m state --state NEW,ESTABLISHED,RELATED -j ACCEPT
sudo iptables -t nat -A PREROUTING \
    -m comment --comment "jaeger:1.22" \
    -p tcp -i eno1 --dport 16686 \
    -j DNAT \
    --to-destination 192.168.127.100:16686
```

Where the exact IP address can be obtained using the `firebuild inspect --profile=... --vmm-id=...` command and the destination IP and interface depend on your configuration, you can use `ip link` to find the `up broadcast` interfaces and relevant IP address. **Tool intergration will be added at a later stage.**

#### how does it work

The builder pulls the requested Docker image with Docker. It then open the Docker image via the Docker `save` command and looks up the `manifest.json` and the Docker image config `json` explicitly stated in the manifest. When config is fetched, a temporary Dockerfile is built from the Docker config history. Any `ADD` and `COPY` commands for resources other than first `/` are used to extract files from the saved source image. When resources are exported, the build further continues exactly the same way as in case of the `Dockerfile` build.

### terminating a daemonized VM

A VM started with the `--daemonize` flag can be stopped in three ways:

- by executing the `kill` tool command, this is a clean stop which will take care of all the necessry clean up
- by executing `reboot` from inside of the VM SSH connection; unclean stop, manual purge of the CNI cache, jailer directory, run cache and the veth link is needed
- by executing the cURL HTTP against the VM socket file; unclean stop, manual purge of the CNI cache, jailer directory, run cache and the veth link is needed

#### VM kill command

To get the VM ID, look closely at the output of the `run ... --detached` command:

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

Copy the VM ID from the output and run:

```sh
sudo $GOPATH/bin/firebuild kill --profile=standard --vmm-id=${VMMID}
```

#### purging the remains of the VMs stopped without the kill command

If a VM exits in any other way than via `kill` command, following data continues residing on the host:

- jail directory with all contents
- run cache directory with all contents
- CNI interface with CNI cache directory

To remove this data, run the `purge` command. 

```sh
sudo $GOPATH/bin/firebuild purge --profile=standard
```

#### list VMs

```sh
sudo $GOPATH/bin/firebuild ls --profile=standard
```

Example output:

```
2021-03-12T01:46:21.752Z [INFO]  ls: vmm: id=df45b6e14538456286e4a4bc1f9bf6e2 running=true pid=20658 image=tests/postgres:13 started="2021-03-12 01:46:11 +0000 UTC" ip-address=192.168.127.9
```

### Dockerfile git+http(s):// URL

It's possible to reference a `Dockerfile` residing in the git repository available under a HTTP(s) URL. Here's an example:

```sh
sudo $GOPATH/bin/firebuild rootfs \
    --profile=standard \
    --dockerfile=git+https://github.com/hashicorp/docker-consul.git:/0.X/Dockerfile#master \
    --cni-network-name=machine-builds \
    --vmlinux-id=vmlinux-v5.8 \
    --tag=combust-labs/consul:1.9.4
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

### supported Dockerfile URL formats

- `http://` and `https://` for direct paths to the `Dockerfile`, these can handle single file only and do not attempt loading any resources handled by `ADD` / `COPY` commands, the server must be capable of responding to `HEAD` and `GET` http requests, more details in `Caveats when building from the URL` further in this document
- special `git+http://` and `git+https://`, documented above
- standard `ssh://`, `git://` and `git+ssh://` URL formats with the expectation that the path meets the criteria from the `git+http(s):// URL` section above

### caveats when building from the URL

The `build` command will resolve the resources referenced in `ADD` and `COPY` commands even when loading the `Dockerfile` via the URL. The context root in this case will be established by removing the file name from the URL. An example:

- consider the URL `https://raw.githubusercontent.com/hashicorp/docker-consul/master/0.X/Dockerfile`
- the `Dockerfile` name will be removed from the URL and the context is `https://raw.githubusercontent.com/hashicorp/docker-consul/master/0.X`
- assuming that the `Dockerfile` contains `ADD ./docker-entrypoint.sh ...`, the resolver will try loading `https://raw.githubusercontent.com/hashicorp/docker-consul/master/0.X/docker-entrypoint.sh`

There are following limitations when loading the resources like that via URL:

- if the `ADD` or `COPY` points to a directory, the command will fail because there is no unified way of loading directories via HTTP, the resolver will not even attempt this, it will most likely fail on the `HTTP GET` request
- the file permissions will not be carried over because there is no method to infer file mode from a HTTP response

### unsupported Dockerfile features

The build program does not support:

- `ONBUILD` commands
- `HEALTHCHECK` commands
- `STOPSIGNAL` commands

### multi-stage Dockerfile builds

`firebuild` supports multi-stage `Dockerfile` builds. An example with [grepplabs Kafka Proxy](https://github.com/grepplabs/kafka-proxy).

Build `v0.2.8` using git repository link:

```sh
sudo $GOPATH/bin/firebuild rootfs \
    --profile=standard \
    --dockerfile=git+https://github.com/grepplabs/kafka-proxy.git:/Dockerfile#v0.2.8 \
    --cni-network-name=machine-builds \
    --vmlinux-id=vmlinux-v5.8 \
    --tag=combust-labs/kafka-proxy:0.2.8
```

### tracing

**TODO: eat your own dog food, start with firebuild.**

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

### license

Unless explcitly stated: AGPL-3.0 License.

