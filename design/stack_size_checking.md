# Type Checking

Our concatenative language is set up for static stack size checking.
We are not allowing any functions to have a runtime or dynamic stack size changing behavior.

For example:

```
[... unknown length list] splat   # Not allowed, since we won't know how many element put on stack
[... unknown length list] 2unpack # allowed since we know it must be 2, but runtime error if not sized correctly.
```

## Important Special cases

For conditional branches, the inner quotes must be evaluated recursively, and their results must be equivalent.

For match arms, they also must have the stack size remain the same before and after.

For loops, the stack size must not have a net change.

## Declaring types

```
type myType = int | str
```

No constructor names given to the different branches. We rarely have cases where a type like below is required.

```
type length = int Inches | int Feet
```

## Function overloading

I think it is convenient and allowed to have overloaded user definitions.
The definitions can even have different arity.

```
def mydef (str -- str)

end

def mydef (float float -- str)

end
```


## Cast syntax

Right now, I'm liking:

```
as <Type statement>
```

Types are made of identifier tokens, which are not allowed outside of lists, so we should be able to determine the end of the type signature.


## Failures

Because this is meant to replace shell scripts, IO operations can often be a significant portion of what we are doing.
So a full effect system would not be desirable.

But I do want something to have a sort of 'catch' all for functions that can fail.

Thinking about marking functions with a `fail` property.

Those functions then would need to be followed by:

1. A try block that represents what to do on failure
2. Syntax to immediately stop execution and exit

```mshell
"out.txt" "data" writeFile
try
    fail @e -> e log 1 exit
end
```

## Defers

Since we are often doing things with files, we often may want a cleanup step.

As part of the "file" construct, there could be a 'defer' block that exists anywhere.



## References:

[Zig diagnostic pattern](https://mikemikeb.com/blog/zig_error_payloads)
