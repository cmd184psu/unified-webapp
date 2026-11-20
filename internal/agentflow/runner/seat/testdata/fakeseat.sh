#!/bin/sh
# A fake interactive seat: it reads the one line agentflow types, finds RESULT.json in it and
# answers as SEAT_MODE says. done: report at once. question: ask, read the answer, then report.
# wait: report once the file named by SEAT_GO exists. silent: never report. exit: quit.
# halt: show a takeover prompt until SEAT_GO exists, then clear the screen and act as done.
# deafonce: swallow the first line without showing it (a TUI still drawing), then as done.
if [ "$SEAT_MODE" = halt ]; then
  # like freebuff when its one session is live elsewhere: wait for the owner, then clear
  echo "Your paid session is in use elsewhere. Take over to move it here"
  while [ ! -f "$SEAT_GO" ]; do sleep 0.01; done
  printf '\f'
  SEAT_MODE=done
fi
echo "fake seat ready"
if [ "$SEAT_MODE" = deafonce ]; then read -r dropped; SEAT_MODE=done; fi
read -r line
echo "got: $line"
result=$(printf '%s\n' "$line" | sed -n 's/.*write \(.*RESULT\.json\) as.*/\1/p')
case "$SEAT_MODE" in
done)
  printf '{"status":"done","summary":"did it","files_changed":["a.go"]}' > "$result" ;;
question)
  printf '{"status":"question","question":"tabs or spaces?"}' > "$result"
  read -r answer
  echo "answer: $answer"
  printf '{"status":"done","summary":"used %s"}' "$answer" > "$result" ;;
spinner)
  # never reports; a ticking status line changes the screen all the time
  i=0; while true; do i=$((i+1)); printf 'working %ss\n' "$i"; sleep 0.02; done ;;
wait)
  while [ ! -f "$SEAT_GO" ]; do sleep 0.01; done
  printf '{"status":"done","summary":"after the restart"}' > "$result" ;;
exit)
  exit 3 ;;
esac
while true; do sleep 1; done
