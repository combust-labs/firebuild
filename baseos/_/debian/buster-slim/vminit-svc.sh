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

PATH=/sbin:/bin

. /lib/init/vars.sh
. /lib/lsb/init-functions

do_start () {
    [ "$VERBOSE" != no ] && log_action_begin_msg "Running vminit"
    /usr/bin/vminit
	ES=$?
	[ "$VERBOSE" != no ] && log_action_end_msg $ES
    # we most likely run after hostname service so just to be on the safe side:
    /etc/init.d/hostname.sh start
	exit $ES
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
	# No-op
	;;
  status)
	exit 0
	;;
  *)
	echo "Usage: vminit-svc.sh [start|stop]" >&2
	exit 3
	;;
esac

exit 0