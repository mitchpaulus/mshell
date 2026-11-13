# Check that examples.txt is what the oracle prints today (the example types
# are extracted from the proof), then run the oracle on its queries and
# compare with the expected answers.
# Messages go to stderr: stdout would become a file named test.
['redo-ifchange' 'oracle']!

['./oracle' '--examples'] * ! `examples.txt` readFile = not if
    "examples.txt is out of date: redo update-examples" wle
    1 exit
end

`examples.txt` readFile lines (l! @l "#" startsWith not @l len 0 > and) filter (tsplit) map examples!
['./oracle'] @examples (:0:) map unlines < * ! lines got!

@got len @examples len = not if
    $"oracle gave {@got len} answers to {@examples len} queries" wle
    1 exit
end

@examples @got (e!, g!
    @e :1: @g = if "" else $"{@e :0:}\n  want {@e :1:}\n  got  {@g}" end
) zip (len 0 >) filter wrong!

@wrong len 0 > if
    @wrong (wle) each
    $"oracle: {@wrong len} of {@examples len} examples disagree" wle
    1 exit
end
$"oracle: {@examples len} examples agree" wle
