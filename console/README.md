# Console source

The canonical frontend lives in
[`gm/internal/services/gmtool/wwwroot/console`](../gm/internal/services/gmtool/wwwroot/console)
and is embedded in the standalone GM binary. There is no generated asset copy.

- `/console/`: authenticated account, character and item directory.
- `/console/preview.html`: isolated visual prototype with sample data.
- `python scripts/preview_console.py`: loopback visual preview.

Through the private Hub, use `/gm/console/`. The public player gateway does not
serve management endpoints. Theme and font notices travel with the assets.
