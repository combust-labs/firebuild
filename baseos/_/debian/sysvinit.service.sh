#!/bin/sh
# This program will be executed with sudo.
# The command environment is written to /etc/firebuild/cmd.env.

cat << 'EOF' > /etc/init.d/DockerEntrypoint.sh
#!/bin/sh

# source the environment file:
. /etc/firebuild/cmd.env
# make sure /dev/fd points to the right place:
[ ! -d /dev/fd ] && ln -s /proc/self/fd /dev/fd

# set the name of the service
SNAME=DockerEntrypoint

start() {
    echo "Starting ${SNAME} ..."
    nohup /bin/sh -c ". /etc/firebuild/cmd.env && cd ${SERVICE_WORKDIR} && ${SERVICE_ENTRYPOINT} ${SERVICE_CMDS} > /var/log/${SNAME}.log 2>&1" &>/var/log/${SNAME}-nohup.log
    echo "${SNAME} started."
    exit 0
}

stop() {
    echo "${SNAME} does not support automatic stop implementation."
    exit 0
}

status() {
    echo "${SNAME} status can't be determined."
    exit 0
}

case "$1" in
start)
    start
    ;;
stop)
    stop
    ;;
reload|restart)
    stop
    start
    ;;
status)
    status
    ;;
*)
    echo $"\nUsage: $0 {start|stop|restart|status}"
    exit 1
esac
EOF

# make the service file executable:
chmod +x /etc/init.d/DockerEntrypoint.sh
# enable service:
update-rc.d DockerEntrypoint.sh defaults