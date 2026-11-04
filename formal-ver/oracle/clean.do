# Remove the oracle and .build, which holds only files.
`oracle` fileExists if `oracle` rm end
`.build` fileExists if
    `.build` lsDir (rm) each
    `.build` rm
end
