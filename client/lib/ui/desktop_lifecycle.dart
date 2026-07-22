import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';
import 'package:tray_manager/tray_manager.dart';
import 'package:window_manager/window_manager.dart';

import '../app_controller.dart';

class DesktopLifecycle extends StatefulWidget {
  const DesktopLifecycle({
    super.key,
    required this.controller,
    required this.child,
  });

  final AppController controller;
  final Widget child;

  @override
  State<DesktopLifecycle> createState() => _DesktopLifecycleState();
}

class _DesktopLifecycleState extends State<DesktopLifecycle>
    with WindowListener, TrayListener {
  bool _exiting = false;

  @override
  void initState() {
    super.initState();
    if (Platform.isWindows) unawaited(_initializeDesktop());
  }

  Future<void> _initializeDesktop() async {
    windowManager.addListener(this);
    trayManager.addListener(this);
    await trayManager.setIcon(await _desktopTrayIconPath());
    await trayManager.setToolTip('NexDrop');
    await trayManager.setContextMenu(
      Menu(
        items: [
          MenuItem(key: 'show', label: '開啟 NexDrop'),
          MenuItem.separator(),
          MenuItem(key: 'exit', label: '完全退出'),
        ],
      ),
    );
  }

  @override
  void onWindowClose() => unawaited(_exit());

  @override
  void onWindowMinimize() => unawaited(windowManager.hide());

  @override
  void onTrayIconMouseDown() => unawaited(_showWindow());

  @override
  void onTrayMenuItemClick(MenuItem menuItem) {
    switch (menuItem.key) {
      case 'show':
        unawaited(_showWindow());
        break;
      case 'exit':
        unawaited(_exit());
        break;
    }
  }

  Future<void> _showWindow() async {
    await windowManager.show();
    await windowManager.focus();
  }

  Future<void> _exit() async {
    if (_exiting) return;
    _exiting = true;
    try {
      await windowManager.setPreventClose(false);
      await widget.controller.shutdown();
      await trayManager.destroy();
      await windowManager.destroy();
    } finally {
      exit(0);
    }
  }

  @override
  void dispose() {
    if (Platform.isWindows) {
      windowManager.removeListener(this);
      trayManager.removeListener(this);
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

Future<String> _desktopTrayIconPath() async {
  final data = await rootBundle.load('windows/runner/resources/app_icon.ico');
  final directory = await getApplicationSupportDirectory();
  final file = File(
    '${directory.path}${Platform.pathSeparator}nexdrop-tray.ico',
  );
  final bytes = data.buffer.asUint8List(data.offsetInBytes, data.lengthInBytes);
  if (!await file.exists() || await file.length() != bytes.length) {
    await file.writeAsBytes(bytes, flush: true);
  }
  return file.path;
}
