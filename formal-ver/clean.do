# Remove the compiled proof, .build, and the side files rocq leaves when run
# by hand.
["*.vo" "*.vos" "*.vok" "*.glob" ".*.aux"] (glob (rm) each) each
# .build holds one directory of files per target.
`.build` fileExists if
    `.build` lsDir (d! @d lsDir (rm) each @d rm) each
    `.build` rm
end
