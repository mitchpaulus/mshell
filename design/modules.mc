{
  "version": 1,
  "grid": 8.0,
  "preamble": "",
  "page": "3affc76a",
  "pages": [
    {
      "id": "3affc76a",
      "name": "Page 1",
      "view": {
        "x": 49.042991638183594,
        "y": 356.59881591796875,
        "zoom": 0.924077033996582
      },
      "nodes": [
        {
          "id": "1fc9a342",
          "type": "typst",
          "x": 104.0,
          "y": 64.0,
          "width": 320.0,
          "height": null,
          "source": "= Modules"
        },
        {
          "id": "eabcbb45",
          "type": "typst",
          "x": 104.0,
          "y": 104.0,
          "width": 416.0,
          "height": null,
          "source": "= Goals\n\n- Simple single files stay simple, no std import line\n- The file is the unit of identity, identified by crypto hash\n- Don't like shadowing at all\n- Qualified only"
        },
        {
          "id": "e932fda1",
          "type": "typst",
          "x": 104.0,
          "y": 232.0,
          "width": 320.0,
          "height": null,
          "source": "- Modules are only definitions\n- Cannot modify envirnoment variables?"
        },
        {
          "id": "d8bb941e",
          "type": "typst",
          "x": 584.0,
          "y": -120.0,
          "width": 320.0,
          "height": null,
          "source": "hash -> source"
        },
        {
          "id": "a128d093",
          "type": "typst",
          "x": 112.0,
          "y": -144.0,
          "width": 392.0,
          "height": null,
          "source": "Source files are by definition: UTF-8 encoded, no BOM, no requirements on line endings, but standard would be unix line endings"
        },
        {
          "id": "979c7d74",
          "type": "typst",
          "x": 584.0,
          "y": -88.0,
          "width": 376.0,
          "height": null,
          "source": "Modules and scripts are explicitly different, and are marked as such. Modules get MODULE keyword `module`.\n\n- Modules have strict lists\n- only defs\n- VER only makes sense at the top level\n- maybe limit modules from modifying env vars?"
        },
        {
          "id": "dec9d61b",
          "type": "typst",
          "x": 584.0,
          "y": 88.0,
          "width": 320.0,
          "height": null,
          "source": "Module grammar:\n\n```\nMODULE defs* export-line\n```"
        },
        {
          "id": "94f13f69",
          "type": "typst",
          "x": 992.0,
          "y": -128.0,
          "width": 576.0,
          "height": null,
          "source": "#table(\n  columns: (2fr, 1fr, 1fr),\n  [*File*], [*May import by path?*], [*May import by hash?*],\n  [The script you run], [yes], [yes],\n  [A file reached only through path imports, at any depth], [yes], [yes],\n  [A file reached through a hash, and everything below it], [no], [yes],\n)"
        }
      ]
    }
  ]
}
