FROM alpine:3.13

ARG VMINIT_URI=https://github.com/combust-labs/firebuild-mmds/releases/download
ARG VMINIT_VERSION=0.0.12
ADD ${VMINIT_URI}/v${VMINIT_VERSION}/vminit-linux-amd64-${VMINIT_VERSION} /usr/bin/vminit

ADD vminit-svc /etc/init.d/vminit-svc
RUN chmod +x /usr/bin/vminit \
	&& apk update \
	&& apk add openrc openssh sudo util-linux \
	&& ssh-keygen -A \
	&& mkdir -p /home/alpine/.ssh \
	&& touch /home/alpine/.ssh/authorized_keys \
	&& addgroup -S alpine && adduser -S alpine -G alpine -h /home/alpine -s /bin/sh \
	&& echo "alpine:$(cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n1)" | chpasswd \
	&& echo '%alpine ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/alpine \
	&& chown -R alpine:alpine /home/alpine \
	&& chmod 0740 /home/alpine \
	&& chmod 0700 /home/alpine/.ssh \
	&& chmod 0400 /home/alpine/.ssh/authorized_keys \
	&& mkdir -p /run/openrc \
	&& touch /run/openrc/softlevel \
	&& echo ttyS0 > /etc/securetty \
	&& ln -s agetty /etc/init.d/agetty.ttyS0 \
	&& chmod +x etc/init.d/vminit-svc \
	&& rc-update add vminit-svc default \
	&& rc-update add agetty.ttyS0 default \
	&& rc-update add devfs boot \
	&& rc-update add procfs boot \
	&& rc-update add sysfs boot \
	&& rc-update add sshd