#!/bin/sh
#
#       /etc/rc.d/init.d/ferry
#
#       ferry daemon
#
# chkconfig:   2345 95 05
# description: a ferry script

### BEGIN INIT INFO
# Provides:       ferry
# Required-Start:
# Required-Stop:
# Should-Start:
# Should-Stop:
# Default-Start: 2 3 4 5
# Default-Stop:  0 1 6
# Short-Description: ferry
# Description: ferry
### END INIT INFO

cd "$(dirname "$0")"

test -f .env && . $(pwd -P)/.env

_start() {
    setcap 'cap_net_bind_service=ep' ferry
    test $(ulimit -n) -lt 100000 && ulimit -n 100000
    (env ENV=${ENV:-development} is_supervisor_process=1 $(pwd)/ferry) <&- >ferry.error.log 2>&1 &
    local pid=$!
    echo -n "Starting ferry(${pid}): "
    sleep 1
    if (ps ax 2>/dev/null || ps) | grep "${pid} " >/dev/null 2>&1; then
        echo "OK"
    else
        echo "Failed"
    fi
}

_stop() {
    local pid="$(pidof ferry)"
    if test -n "${pid}"; then
        echo -n "Stopping ferry(${pid}): "
        if kill ${pid}; then
            echo "OK"
        else
            echo "Failed"
        fi
    fi
}

_restart() {
    _stop
    sleep 1
    _start
}

_reload() {
    pkill -HUP -o -x ferry
}

_usage() {
    echo "Usage: [sudo] $(basename "$0") {start|stop|reload|restart}" >&2
    exit 1
}

_${1:-usage}
