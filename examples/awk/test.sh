#!/bin/bash

emp_test() {
    if diff <(awk -f "$1".awk emp.data) <(mshell "$1".msh < emp.data); then
        printf "%s. pass\n" "$1"
    else
        printf "%s. fail\n" "$1"
        FAIL=1
    fi
}

FAIL=0

emp_test 1

if diff <(seq 1 20 | awk -f '2.awk' ) <(seq 1 20 | mshell 2.msh); then
    printf "2. pass\n"
else
    printf "2. fail\n"
    FAIL=1
fi

emp_test 3
emp_test 4


if diff <(awk -f '5.awk' < 5.data) <(mshell 5.msh < 5.data); then
    printf "5. pass\n"
else
    printf "5. fail\n"
    FAIL=1
fi

exit "$FAIL"