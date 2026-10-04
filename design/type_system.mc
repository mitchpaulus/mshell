{
  "version": 1,
  "grid": 8.0,
  "preamble": "",
  "page": "b7b0a21e",
  "pages": [
    {
      "arrows": [
        {
          "id": "bd092523",
          "from": "0b38faa5",
          "from_side": "south",
          "to": "d3d2e80f",
          "to_side": "north",
          "routing": "orthogonal",
          "trunk": {
            "id": "7df0a8d3",
            "vertical": false,
            "coordinate": 367.690673828125,
            "start": 623.5,
            "end": 624.5
          }
        },
        {
          "id": "a747ccae",
          "from": "0b38faa5",
          "from_side": "south",
          "to": "45d57d02",
          "to_side": "north",
          "routing": "orthogonal",
          "trunk": {
            "id": "7df0a8d3",
            "vertical": false,
            "coordinate": 367.690673828125,
            "start": 623.5,
            "end": 624.5
          }
        }
      ],
      "id": "b7b0a21e",
      "name": "Page 1",
      "view": {
        "x": 105.05860900878906,
        "y": 117.84796142578125,
        "zoom": 0.8264464139938354
      },
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
          "x": 368.0,
          "y": 480.0,
          "width": 320.0,
          "height": null,
          "source": "Because we have reference semantics, containers need to be \"invariant\""
        },
        {
          "id": "45d57d02",
          "type": "typst",
          "x": 808.0,
          "y": 480.0,
          "width": 320.0,
          "height": null,
          "source": "= Type Alias\n\nnon-recursive shorthand for expansion"
        }
      ]
    }
  ]
}
