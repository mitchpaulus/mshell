{
  "version": 0,
  "grid": 8.0,
  "view": {
    "x": -76.57182312011719,
    "y": 16.887065887451172,
    "zoom": 1.0
  },
  "preamble": "",
  "nodes": [
    {
      "id": "d3afe5d4",
      "type": "typst",
      "x": 128.0,
      "y": 48.0,
      "width": 320.0,
      "height": null,
      "source": "= `mshell` typing"
    },
    {
      "id": "b220204b",
      "type": "typst",
      "x": 128.0,
      "y": 112.0,
      "width": 320.0,
      "height": null,
      "source": "= Primitive types\n\n"
    },
    {
      "id": "2a68f36d",
      "type": "typst",
      "x": 128.0,
      "y": 184.0,
      "width": 320.0,
      "height": null,
      "source": "== Non container\n\n#table(\n  columns: (auto, auto),\n  \n  table.header([Type], [Value/Reference]),\n  [string], [value],\n  [path], [value],\n  [integer], [value],\n  [float], [value],\n  [bool], [value],\n[datetime], [value],\n\n\n)\n"
    },
    {
      "id": "0b38faa5",
      "type": "typst",
      "x": 464.0,
      "y": 184.0,
      "width": 320.0,
      "height": null,
      "source": "== Container\n\n#table(\n  columns: (auto, auto),\n  \n  table.header([Type], [Value/Reference]),\n  [list/pipe], [reference],\n  [quote], [reference],\n  [dict], [reference],\n  [grid], [reference],\n\n\n\n)"
    },
    {
      "id": "b3a9948a",
      "type": "typst",
      "x": 816.0,
      "y": 184.0,
      "width": 320.0,
      "height": null,
      "source": "= `enum`\n\n`enum Maybe = none | just v end`"
    },
    {
      "id": "d3d2e80f",
      "type": "typst",
      "x": 480.0,
      "y": 368.0,
      "width": 320.0,
      "height": null,
      "source": "Because we have reference semantics, containers need to be \"invariant\""
    },
    {
      "id": "45d57d02",
      "type": "typst",
      "x": 472.0,
      "y": 504.0,
      "width": 320.0,
      "height": null,
      "source": "= Type Alias\n\nnon-recursive shorthand for expansion"
    }
  ]
}
