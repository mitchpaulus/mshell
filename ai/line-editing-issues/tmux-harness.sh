#!/bin/bash
# tmux harness: h.sh start COLS ROWS | send <keys...> | hex <hexbytes> | screen | cursor | stop
S=/tmp/claude-1000/-home-mitch-repos-mshell/fec2a30a-9c8d-4089-8ebd-5f9d394a95eb/scratchpad
T="tmux -L mshaudit"
case "$1" in
  start)
    $T kill-server 2>/dev/null
    for i in 1 2 3 4 5 6 7 8 9 10; do
      sleep 0.3
      if $T -f /dev/null new-session -d -x "$2" -y "$3" -e HOME=$S/home -e XDG_DATA_HOME=$S/data -e XDG_CONFIG_HOME=$S/cfg -e MSHSTDLIB=/home/mitch/repos/mshell/lib/std.msh -e TERM=xterm-256color "$S/msh" 2>/dev/null; then break; fi
    done
    sleep 0.6 ;;
  send) shift; $T send-keys -l "$@"; sleep 0.3 ;;
  key) shift; $T send-keys "$@"; sleep 0.3 ;;
  hex) shift; $T send-keys -H "$@"; sleep 0.3 ;;
  screen) $T capture-pane -p | cat -A | sed 's/\$$/|/' ;;
  cursor) $T display -p '#{cursor_x} #{cursor_y} #{pane_width}x#{pane_height}' ;;
  resize) $T resize-window -x "$2" -y "$3"; sleep 0.5 ;;
  stop) $T kill-server 2>/dev/null ;;
esac
