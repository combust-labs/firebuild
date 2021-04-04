#! /bin/sh
### BEGIN INIT INFO
# Provides:          vminit-svc
# Required-Start:    $network
# Required-Stop:
# Should-Start:      
# Default-Start:     2 3 4
# Default-Stop:      0 1 6
# Short-Description: Run vminit program on boot
# Description:       Contacts MMDS service as a guest and
#                    initializes the VM using MMDS metadata.
### END INIT INFO

PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
pidfile="/var/run/firebuild-entrypoint.pid"

. /lib/init/vars.sh
. /lib/lsb/init-functions

do_start () {

	# do other common init tasks:
	ln -s /proc/self/fd /dev/fd
	chmod 1777 /tmp

    [ "$VERBOSE" != no ] && log_action_begin_msg "Running vminit"
	# init from MMDS
	/usr/bin/vminit	
	# find what is the firebuild executor file:
	executor=$(/usr/bin/vminit --print-flags | /bin/grep path-entrypoint-runner-file | /usr/bin/awk '{print $2}')
	# we are most likely running after the hostname.sh, if it exists
	# so let's rerun it just to be safe:
	if [ -f /etc/init.d/hostname.sh ]; then
	    /etc/init.d/hostname.sh start
	fi
	# if there is an executor:
	if [ -f "${executor}" ]; then
		mkdir -p /var/log
		# run it and log to the file:
		("${executor}" >/var/log/firebuild-entrypoint.log 2>&1)&
		# get the pid of the subshell:
		mypid=$!
		# write the pid to the file:
		mkdir -p $(/usr/bin/dirname "${pidfile}")
		/bin/echo ${mypid} > "${pidfile}"
	fi
	exit 0
}

do_stop () {
	[ "$VERBOSE" != no ] && log_action_begin_msg "Stopping vminit"
	if [ -f "${pidfile}" ]; then
		# read the pid from file:
		mainpid=$(/bin/cat "${pidfile}")
		# if any process has been started by the main process, stop it:
		for subpid in $(/usr/bin/pgrep -P ${mainpid}); do
				/bin/kill ${subpid}
		done
		# stop the main process:
		/bin/kill $mainpid
		# remove the pid file:
		/bin/rm "${pidfile}"
	fi
	exit 0
}

case "$1" in
  start|"")
	do_start
	;;
  restart|reload|force-reload)
	echo "Error: argument '$1' not supported" >&2
	exit 3
	;;
  stop)
	do_stop
	;;
  status)
	exit 0
	;;
  *)
	echo "Usage: vminit-svc.sh [start|stop]" >&2
	exit 3
	;;
esac
