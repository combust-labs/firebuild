#!/bin/sh
# This program will be executed with sudo.
# The command to execute is written to an /etc/firebuild/cmd.env file.

cat << 'EOF' > /etc/local.d/DockerEntrypoint.start
mkdir -p /var/log
. /etc/firebuild/cmd.env
nohup /bin/sh -c "cd ${SERVICE_WORKDIR} && ${SERVICE_ENTRYPOINT} ${SERVICE_CMDS} &>/var/log/consul.log" &
EOF

cat << 'EOF' > /etc/local.d/DockerEntrypoint.stop
#!/bin/sh
. /etc/firebuild/cmd.env
pid=$(ps | grep -v grep | grep "${SERVICE_ENTRYPOINT}" | awk '{print $1}')
kill -0 $pid
checkStatus=$?
if [ $checkStatus -eq 0 ]; then
    echo "stopping service"
    kill "${pid}"
else
    echo "there was no service to stop"
fi
EOF

chmod +x /etc/local.d/DockerEntrypoint.start
chmod +x /etc/local.d/DockerEntrypoint.stop