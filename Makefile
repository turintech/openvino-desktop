# Makefile for OpenVINO Desk — works on Windows (cmd.exe) and Linux/macOS

ifeq ($(OS),Windows_NT)
    UV_ZIP    = uv-x86_64-pc-windows-msvc.zip
    UV_BIN    = uv.exe
    PYTHON    = export\Scripts\python.exe
    RM_RF     = rmdir /s /q
    MKDIR_P   = mkdir
    CP        = copy
    RM        = del
    PATHSEP   = \\
else
    UV_ZIP    = uv-x86_64-unknown-linux-gnu.tar.gz
    UV_BIN    = uv
    PYTHON    = export/Scripts/python
    RM_RF     = rm -rf
    MKDIR_P   = mkdir -p
    CP        = cp
    RM        = rm -f
    PATHSEP   = /
endif

.PHONY: help install export run clean get-remote-export openvino build-cli ui-dev ui-build

help:
	@echo Available commands:
	@echo   make install            - Create venv and install dependencies
	@echo   make export             - Run model export script
	@echo   make run                - Start OVMS server
	@echo   make clean              - Remove venv and python
	@echo   make get-remote-export  - Download and extract latest export release
	@echo   make openvino           - Download and extract OVMS server
	@echo   make build-cli          - Build the Go CLI binary
	@echo   make ui-dev             - Run Wails UI in development mode
	@echo   make ui-build           - Build Wails UI as a Windows executable

install:
	./$(UV_BIN) python install 3.12.12 --install-dir ./python
	./$(UV_BIN) venv export --python ./python/cpython-3.12.12-windows-x86_64-none/python.exe --relocatable
	./$(UV_BIN) pip install --python $(PYTHON) -r ui/assets/export-model-requirements/requirements.txt

export:
	$(PYTHON) ui/assets/export-model-requirements/export_model.py $(ARGS)

run:
	ovms/setupvars.ps1 && ovms/ovms.exe --rest_port 8000 --config_path config.json

openvino:
	-$(RM_RF) ovms
	curl -L https://github.com/openvinotoolkit/model_server/releases/download/v2026.0/ovms_windows_python_on.zip -o ovms-tmp.zip
	tar -xf ovms-tmp.zip
	$(RM) ovms-tmp.zip

build-cli:
	cd cli && go build -o ../openvino-cli.exe .

appicon:
	-$(MKDIR_P) ui$(PATHSEP)build$(PATHSEP)windows
	$(CP) ui$(PATHSEP)appicon.png ui$(PATHSEP)build$(PATHSEP)appicon.png
	$(CP) ui$(PATHSEP)logo.ico ui$(PATHSEP)build$(PATHSEP)windows$(PATHSEP)icon.ico

ui-dev: appicon
	cd ui && wails dev

ui-build: appicon
	cd ui && wails build

get-remote-export:
	-$(RM_RF) export
	curl -L https://github.com/turintech/openvino-desktop/releases/latest/download/export-windows.zip -o export-windows.zip
	tar -xf export-windows.zip
	$(RM) export-windows.zip

