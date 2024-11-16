
#!/bin/bash
set -e

# Redirect all output to a log file.
exec 1> >(tee /tmp/deltek_project.log) 2>&1

printf 'Starting at %s\n' "$(date)"

# Absolute paths here because this is in a cron job that doesn't have my full PATH.
DIR='/home/azureuser/deltek'

if ! test -d "$DIR"; then
    echo "Directory $DIR does not exist"
    exit 1
fi
OUTFILE="$DIR"/"$(date +%Y-%m-%d)".xlsx

if test -f "$OUTFILE"; then
    OUTFILE="$DIR"/"$(date '+%Y-%m-%d %H%M%S')".xlsx
fi

"$HOME"/.local/bin/ccllc-deltek projDump > /tmp/deltek.tsv

printf 'Dump finished. Cleaning and writing to Excel.\n'
# python3 "$DIR"/to_html.py < /tmp/deltek.tsv > "$DIR"/memos.html
python3 "$DIR"/clean.py < /tmp/deltek.tsv > tmp && mv tmp /tmp/deltek.tsv

"$HOME"/.local/bin/xlwrite -e block A1 /tmp/deltek.tsv "$OUTFILE"
# pandoc -o "$DIR"/memos.docx "$DIR"/memos.html

python3 "$DIR"/marketing.py | "$HOME"/.local/bin/xlwrite -c -w "Marketing" block A1 - "$OUTFILE"

printf 'Finished writing to "%s" at %s.\n' "$OUTFILE" "$(date)"

"$HOME"/.local/bin/CCLLCParser --blob-rename --subject 'Deltek Project Data Dump' upload "$OUTFILE"

rm "$OUTFILE"
