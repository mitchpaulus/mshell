#!/bin/bash

emp_test() {
    if diff <(awk -f "$1".awk emp.data) <(mshell "$1".msh < emp.data); then
        printf "%s. pass\n" "$1"
    else
        printf "%s. fail\n" "$1"
        FAIL=1
    fi
}

data_test() {
    if diff <(awk -f "$1".awk "$1".data) <(mshell "$1".msh < "$1".data); then
        printf "%s. pass\n" "$1"
    else
        printf "%s. fail\n" "$1"
        FAIL=1
    fi
}

FAIL=0

emp_test 1
data_test 2
emp_test 3
emp_test 4
data_test 5
emp_test 6
emp_test 7
emp_test 8
data_test 9
data_test 10
data_test 11
emp_test 12
emp_test 13
emp_test 14
emp_test 15
emp_test 16
emp_test 17
data_test 18
data_test 19
data_test 20

exit "$FAIL"
