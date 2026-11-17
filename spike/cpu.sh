#!/bin/sh
# cpu.sh <pid> <secs> : average CPU of a process over a window, in % of one core
a=$(awk '{print $14+$15}' /proc/$1/stat); sleep $2; b=$(awk '{print $14+$15}' /proc/$1/stat)
echo "pid $1 cpu% $(( (b - a) * 100 / $(getconf CLK_TCK) / $2 ))"
