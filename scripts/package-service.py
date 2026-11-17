#!/usr/bin/env python3
"""Build current standalone Go server releases."""
import runpy
from pathlib import Path
runpy.run_path(str(Path(__file__).resolve().parent / 'platform/package-server.py'), run_name='__main__')
