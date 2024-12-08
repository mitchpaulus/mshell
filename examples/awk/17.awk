
{ 
    for (i = NF; i > 0; i = i - 1) printf (i == 1 ? "%s" : "%s "), $i
    printf "\n"
}
