Trying to not be clunky.
<https://mchav.github.io/being-less-clunky/>

[Nushell data frames.](https://www.nushell.sh/book/dataframes.html) Essentially wrap around Polars.

# Grids/Data Frames

We are attempting to base our concepts of data frames for grids from several sources.

- Grids from axon
- Data frames from many places, including pandas or something from R.

Right now, I am leaning toward limiting the types to non-container types, strings, ints, floats, datetimes, etc.

# Goals

In priority order.

- Expressiveness: The data manipulations I need to do should just "flow" from my brain without syntax or semantics blocking me.
- Consistency: Syntax should be consistent with the other portions of the language.
- Performance: Limited overhead, implementation should do whatever tricks to make this built-in construct fast.

In terms of performance, I want to optimize the ingestion from flat files or other file sources.
To best do that, those ingestion functions will be in golang, not part of the standard library.

# API Functions

```
select  (grid [str] -- grid)
exclude (grid [str] -- grid)
gridRenameCol (grid str str -- grid)
groupBy (Grid|GridView [str]:keys [dict]:aggs -- Grid)
derive (grid str dict (gridRow -- any) -- grid)

join      (Grid|GridView Grid|GridView (GridRow -- key) (GridRow -- key) -- Grid)
leftJoin  (Grid|GridView Grid|GridView (GridRow -- key) (GridRow -- key) -- Grid)
outerJoin (Grid|GridView Grid|GridView (GridRow -- key) (GridRow -- key) -- Grid)

# Injestion

toGrid ([[str]] -- grid) # headers assumed on first row.
```

## References

From Petersohn et al. and <https://mchav.github.io/what-category-theory-teaches-us-about-dataframes/>

Operator           Origin	What it does
SELECTION          Relational	Eliminate rows
PROJECTION         Relational	Eliminate columns
UNION              Relational	Combine two dataframes vertically
DIFFERENCE         Relational	Rows in one but not the other
CROSS PRODUCT/JOIN Relational	Combine two dataframes by key
DROP DUPLICATES    Relational	Remove duplicate rows
GROUPBY            Relational Group rows by column values
SORT               Relational	Reorder rows
RENAME             Relational	Rename columns
WINDOW             SQL	Sliding-window functions
TRANSPOSE          Dataframe	Swap rows and columns
MAP                Dataframe	Apply a function to every row
TOLABELS           Dataframe	Promote data to column/row labels
FROMLABELS         Dataframe	Demote labels back to data

<https://arxiv.org/abs/2001.00888>

<https://www2.eecs.berkeley.edu/Pubs/TechRpts/2021/EECS-2021-193.pdf>

<https://www.sumsar.net/blog/pandas-feels-clunky-when-coming-from-r/>
