import 'dart:async';
import 'dart:io';

import 'package:desktop_drop/desktop_drop.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../app_controller.dart';
import 'nexdrop_page.dart';

class SendView extends StatefulWidget {
  const SendView({super.key, required this.controller});
  final AppController controller;
  @override
  State<SendView> createState() => _SendViewState();
}

class _SendViewState extends State<SendView> {
  final content = TextEditingController();
  final selectedDevices = <String>{};
  bool selectionInitialized = false;
  List<String> files = const [];
  String routeMode = 'AUTOMATIC';
  bool draggingFiles = false;
  bool notification = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _applySharedContent());
  }

  @override
  void dispose() {
    content.dispose();
    super.dispose();
  }

  @override
  void didUpdateWidget(covariant SendView oldWidget) {
    super.didUpdateWidget(oldWidget);
    WidgetsBinding.instance.addPostFrameCallback((_) => _applySharedContent());
  }

  void _applySharedContent() {
    final share = widget.controller.takePendingShare();
    if (share == null || !mounted) return;
    setState(() {
      content.text = share.text;
      files = share.files;
    });
  }

  @override
  Widget build(BuildContext context) {
    final trusted = widget.controller.devices
        .where((device) => device.trusted && device.publicKey != null)
        .toList();
    if (!selectionInitialized && trusted.isNotEmpty) {
      selectionInitialized = true;
      selectedDevices.addAll(trusted.map((device) => device.id));
    }
    return CallbackShortcuts(
      bindings: {
        const SingleActivator(LogicalKeyboardKey.enter, control: true):
            _sendFromShortcut,
        const SingleActivator(LogicalKeyboardKey.enter, meta: true):
            _sendFromShortcut,
      },
      child: NexDropPage(
        title: '安全傳送',
        subtitle: '區網可用時直接傳輸，否則交由你的 Linux 節點接力。',
        onRefresh: widget.controller.reload,
        child: DropTarget(
          onDragEntered: (_) => setState(() => draggingFiles = true),
          onDragExited: (_) => setState(() => draggingFiles = false),
          onDragDone: (details) => setState(() {
            draggingFiles = false;
            files = {
              ...files,
              ...details.files.map((file) => file.path),
            }.toList();
          }),
          child: Card(
            color: draggingFiles
                ? Theme.of(context).colorScheme.primaryContainer
                : null,
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  TextField(
                    controller: content,
                    minLines: 4,
                    maxLines: 8,
                    enabled: files.isEmpty,
                    decoration: InputDecoration(
                      labelText: '文字或網址',
                      alignLabelWithHint: true,
                      helperText: Platform.isWindows
                          ? 'Ctrl + Enter 快速傳送'
                          : null,
                    ),
                  ),
                  const SizedBox(height: 12),
                  if (files.isEmpty)
                    SwitchListTile(
                      contentPadding: EdgeInsets.zero,
                      secondary: const Icon(
                        Icons.notifications_active_outlined,
                      ),
                      title: const Text('一般通知訊息'),
                      subtitle: const Text('以通知類型傳送這段文字。'),
                      value: notification,
                      onChanged: (value) =>
                          setState(() => notification = value),
                    ),
                  OutlinedButton.icon(
                    onPressed: _pickFiles,
                    icon: const Icon(Icons.attach_file_rounded),
                    label: Text(
                      files.isEmpty ? '選擇檔案' : '${files.length} 個檔案已選擇',
                    ),
                  ),
                  const SizedBox(height: 22),
                  Text(
                    '信任設備',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: trusted
                        .map(
                          (device) => FilterChip(
                            label: Text(device.displayName),
                            selected: selectedDevices.contains(device.id),
                            onSelected: (_) => setState(
                              () => selectedDevices.contains(device.id)
                                  ? selectedDevices.remove(device.id)
                                  : selectedDevices.add(device.id),
                            ),
                          ),
                        )
                        .toList(),
                  ),
                  const SizedBox(height: 18),
                  DropdownButtonFormField<String>(
                    initialValue: routeMode,
                    decoration: const InputDecoration(labelText: '傳輸路由'),
                    items: const [
                      DropdownMenuItem(
                        value: 'AUTOMATIC',
                        child: Text('自動（區網優先）'),
                      ),
                      DropdownMenuItem(
                        value: 'LAN_ONLY',
                        child: Text('僅區域網路'),
                      ),
                      DropdownMenuItem(
                        value: 'NODE_ONLY',
                        child: Text('僅 Linux 節點'),
                      ),
                      DropdownMenuItem(
                        value: 'WAIT_LAN',
                        child: Text('等待區域網路'),
                      ),
                    ],
                    onChanged: (value) => setState(() => routeMode = value!),
                  ),
                  const SizedBox(height: 24),
                  ValueListenableBuilder<TextEditingValue>(
                    valueListenable: content,
                    builder: (context, value, child) => FilledButton.icon(
                      onPressed: _canSend && !widget.controller.busy
                          ? _send
                          : null,
                      icon: const Icon(Icons.lock_rounded),
                      label: Text(
                        widget.controller.busy ? '加密與傳送中…' : '建立安全傳輸',
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  bool get _canSend =>
      (content.text.trim().isNotEmpty || files.isNotEmpty) &&
      selectedDevices.isNotEmpty &&
      widget.controller.currentDevice?.trusted == true;

  Future<void> _pickFiles() async {
    final result = await FilePicker.pickFiles(
      allowMultiple: true,
      withData: false,
    );
    if (result != null) {
      setState(() => files = result.paths.whereType<String>().toList());
    }
  }

  void _sendFromShortcut() {
    if (_canSend && !widget.controller.busy) _send();
  }

  void _send() {
    final recipients = widget.controller.devices
        .where((device) => selectedDevices.contains(device.id))
        .toList();
    unawaited(
      widget.controller
          .send(
            content: content.text,
            recipients: recipients,
            groupId: null,
            groupAll: false,
            files: files,
            routeMode: routeMode,
            notification: notification,
          )
          .then((_) {
            if (!mounted) return;
            setState(() {
              content.clear();
              files = const [];
              selectedDevices
                ..clear()
                ..addAll(
                  widget.controller.devices
                      .where(
                        (device) => device.trusted && device.publicKey != null,
                      )
                      .map((device) => device.id),
                );
              routeMode = 'AUTOMATIC';
              notification = false;
            });
            ScaffoldMessenger.of(
              context,
            ).showSnackBar(const SnackBar(content: Text('已建立安全傳輸')));
          })
          .catchError((_) {}),
    );
  }
}
