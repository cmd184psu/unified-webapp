#!/bin/sh
# A fake owner-run queue seat: on "check your queue" it reads QUEUE_FILE, finds the RESULT.json
# path agentflow wrote there and acts as QSEAT_MODE says. react: report done. question: ask,
# read the answer, report. deaf: ignore every nudge (prints nothing). halt: show a takeover
# prompt and never react. Empty lines (the nudge's second Enter) are ignored.
if [ "$QSEAT_MODE" = halt ]; then
  echo "Your paid session is in use elsewhere. Take over to move it here"
fi
echo "owner seat idle"
while read -r line; do
  [ -z "$line" ] && continue
  case "$QSEAT_MODE" in deaf|halt) continue ;; esac
  case "$line" in
  "check your queue"*)
    echo "reading the queue"
    result=$(sed -n 's/.*write the file `\(.*RESULT\.json\)`.*/\1/p' "$QUEUE_FILE" | head -1)
    if [ "$QSEAT_MODE" = question ]; then
      printf '{"status":"question","question":"tabs or spaces?"}' > "$result"
      answer=""; while [ -z "$answer" ]; do read -r answer; done
      echo "answer: $answer"
      printf '{"status":"done","summary":"used %s"}' "$answer" > "$result"
    else
      printf '{"status":"done","summary":"queue task done","files_changed":["a.go"]}' > "$result"
    fi
    ;;
  *) echo "got: $line" ;;
  esac
done
