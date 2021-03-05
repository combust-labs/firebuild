FROM debian:buster-slim
RUN apt-get update \
	&& apt-get install -y --no-install-recommends gnupg iputils-ping openssh-server sudo sysvinit-core util-linux \
	&& ssh-keygen -A \
	&& mkdir -p /home/debian/.ssh \
	&& touch /home/debian/.ssh/authorized_keys \
    && useradd -d /home/debian -U -s /bin/bash debian \
	&& echo "debian:$(cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n1)" | chpasswd \
	&& echo '%debian ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/debian \
    && echo 's0:2345:respawn:/sbin/getty -L 115200 ttyS0 vt102' >> /etc/inittab \
	&& ln -s agetty /etc/init.d/agetty.ttyS0 \
	&& echo ttyS0 > /etc/securetty \
	&& update-rc.d bootlogs defaults \
	&& update-rc.d bootmisc.sh defaults \
	&& update-rc.d hostname.sh defaults \
	&& update-rc.d mountall.sh defaults \
	&& update-rc.d mountdevsubfs.sh defaults \
	&& update-rc.d mountkernfs.sh defaults \
	&& update-rc.d procps defaults \
	&& update-rc.d rc.local defaults \
	&& update-rc.d urandom defaults
RUN chown -R debian:debian /home/debian \
	&& chmod 0740 /home/debian \
	&& chmod 0700 /home/debian/.ssh \
	&& chmod 0400 /home/debian/.ssh/authorized_keys