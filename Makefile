# Simple Makefile for OVMS setup on Windows

.PHONY: help install export run clean get-remote-export

help:
	@echo Available commands:
	@echo   make install            - Create venv and install dependencies
	@echo   make export             - Run model export script
	@echo   make run                - Start OVMS server
	@echo   make clean              - Remove venv
	@echo   make get-remote-export  - Download and extract latest export release

install:
	uv python install 3.12 --install-dir python-tmp
	powershell -Command "Remove-Item export -Recurse -Force -ErrorAction SilentlyContinue; Move-Item (Get-ChildItem python-tmp\cpython-* | Select-Object -First 1).FullName export; Remove-Item python-tmp -Recurse -Force"
	powershell -Command "Remove-Item export\Lib\EXTERNALLY-MANAGED -Force -ErrorAction SilentlyContinue"
	uv pip install --system --python export\python.exe -r export-model\requirements.txt

export:
	export\python.exe export-model\export_model.py $(ARGS)

run:
	ovms\setupvars.ps1 && ovms\ovms.exe --rest_port 8000 --config_path config.json

clean:
	rmdir /s /q export

get-remote-export:
	-rmdir /s /q export
	curl -L https://github.com/vaggeliskls/openvino-desk/releases/latest/download/export-windows.zip -o export-windows.zip
	tar -xf export-windows.zip
# del export-windows.zip
