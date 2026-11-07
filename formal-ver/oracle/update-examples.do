# Rewrite examples.txt from the oracle.
# examples.txt is checked in, so this writes it directly instead of being its
# target: redo would make the file read-only and ask before replacing it after
# every checkout.
['redo-ifchange' 'oracle']!
['./oracle' '--examples'] `examples.txt` > !
