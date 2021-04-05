FROM debian:buster-slim

ARG VMINIT_URI=https://github.com/combust-labs/firebuild-mmds/releases/download
ARG VMINIT_VERSION=0.0.12
ADD ${VMINIT_URI}/v${VMINIT_VERSION}/vminit-linux-amd64-${VMINIT_VERSION} /usr/bin/vminit

ADD vminit-svc.sh /etc/init.d/vminit-svc.sh
RUN chmod +x /usr/bin/vminit \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends gnupg iputils-ping openssh-server procps sudo sysvinit-core util-linux \
	&& ssh-keygen -A \
	&& mkdir -p /home/debian/.ssh \
	&& touch /home/debian/.ssh/authorized_keys \
    && useradd -d /home/debian -U -s /bin/bash debian \
	&& echo "debian:$(cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n1)" | chpasswd \
	&& echo '%debian ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/debian \
    && echo 's0:2345:respawn:/sbin/getty -L 115200 ttyS0 vt102' >> /etc/inittab \
	&& chown -R debian:debian /home/debian \
	&& chmod 0740 /home/debian \
	&& chmod 0700 /home/debian/.ssh \
	&& chmod 0400 /home/debian/.ssh/authorized_keys \
	&& ln -s agetty /etc/init.d/agetty.ttyS0 \
	&& echo ttyS0 > /etc/securetty \
	&& chmod +x etc/init.d/vminit-svc.sh \
	&& update-rc.d vminit-svc.sh defaults \
	&& update-rc.d bootlogs defaults \
	&& update-rc.d bootmisc.sh defaults \
	&& update-rc.d hostname.sh defaults \
	&& update-rc.d mountall.sh defaults \
	&& update-rc.d mountdevsubfs.sh defaults \
	&& update-rc.d mountkernfs.sh defaults \
	&& update-rc.d procps defaults \
	&& update-rc.d rc.local defaults \
	&& update-rc.d urandom defaults