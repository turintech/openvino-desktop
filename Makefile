# Simple Makefile for OVMS setup on Windows

.PHONY: help install export run clean

help:
	@echo Available commands:
	@echo   make install  - Create venv and install dependencies
	@echo   make export   - Run model export script
	@echo   make run      - Start OVMS server
	@echo   make clean    - Remove venv

install:
	uv venv export --python 3.12
	uv pip install --python export\Scripts\python.exe -r model-export\requirements.txt

export:
	export\Scripts\python.exe model-export\export_model.py

run:
	ovms\setupvars.ps1 && ovms\ovms.exe --rest_port 8000 --config_path config.json

clean:
	rmdir /s /q export
