FROM alpine:3.13
RUN apk update \
	&& apk add openrc openssh sudo util-linux \
	&& ssh-keygen -A \
	&& mkdir -p /home/alpine/.ssh \
	&& addgroup -S alpine && adduser -S alpine -G alpine -h /home/alpine -s /bin/sh \
	&& echo "alpine:$(cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n1)" | chpasswd \
	&& echo '%alpine ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/alpine \
	&& ln -s agetty /etc/init.d/agetty.ttyS0 \
	&& echo ttyS0 > /etc/securetty \
	&& rc-update add agetty.ttyS0 default \
	&& rc-update add devfs boot \
	&& rc-update add procfs boot \
	&& rc-update add sysfs boot \
	&& rc-update add local default
COPY ./key.pub /home/alpine/.ssh/authorized_keys
RUN chown -R alpine:alpine /home/alpine \
	&& chmod 0740 /home/alpine \
	&& chmod 0700 /home/alpine/.ssh \
	&& chmod 0400 /home/alpine/.ssh/authorized_keys \
	&& mkdir -p /run/openrc \
	&& touch /run/openrc/softlevel \
	&& rc-update add sshd