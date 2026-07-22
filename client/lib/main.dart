import 'dart:async';
import 'dart:io';

import 'package:file_picker/file_picker.dart';
import 'package:desktop_drop/desktop_drop.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:window_manager/window_manager.dart';
import 'package:workmanager/workmanager.dart';

import 'app_controller.dart';
import 'core/api_client.dart';
import 'core/local_database.dart';
import 'core/models.dart';
import 'core/node_join.dart';
import 'core/platform_share.dart';
import 'ui/desktop_lifecycle.dart';

const _backgroundSyncTask = 'nexdrop.background-sync';

@pragma('vm:entry-point')
void backgroundCallbackDispatcher() {
  Workmanager().executeTask((_, _) async {
    final api = ApiClient();
    final database = LocalDatabase();
    try {
      if (!await api.restore()) return true;
      final transfers = await api.transfers();
      for (final transfer in transfers) {
        await database.cacheTransfer(
          id: transfer.id,
          contentType: transfer.contentType,
          route:
              transfer.targets.map((target) => target.route).toSet().length == 1
              ? transfer.targets.first.route
              : 'MIXED',
          status: transfer.status,
          totalBytes: transfer.files.fold<int>(
            0,
            (total, file) => total + file.size,
          ),
          createdAt: transfer.createdAt,
        );
      }
      return true;
    } catch (_) {
      return false;
    } finally {
      await database.close();
      api.close();
    }
  });
}

Future<void> main(List<String> arguments) async {
  WidgetsFlutterBinding.ensureInitialized();
  if (Platform.isAndroid) {
    await Workmanager().initialize(backgroundCallbackDispatcher);
    await Workmanager().registerPeriodicTask(
      _backgroundSyncTask,
      _backgroundSyncTask,
      frequency: const Duration(minutes: 15),
      constraints: Constraints(networkType: NetworkType.connected),
      existingWorkPolicy: ExistingPeriodicWorkPolicy.keep,
    );
  }
  if (Platform.isWindows) {
    await windowManager.ensureInitialized();
    await windowManager.waitUntilReadyToShow(
      const WindowOptions(
        size: Size(1120, 760),
        minimumSize: Size(860, 620),
        center: true,
        title: 'NexDrop',
      ),
      () async {
        await windowManager.setPreventClose(true);
        await windowManager.show();
      },
    );
  }
  final controller = AppController();
  final initialJoin = NodeJoinConfiguration.fromArguments(arguments);
  if (initialJoin != null) controller.queueJoin(initialJoin);
  final shareIndex = arguments.indexOf('--share');
  if (shareIndex >= 0 && shareIndex + 1 < arguments.length) {
    controller.queueShare(
      PlatformSharePayload(
        text: '',
        files: arguments.skip(shareIndex + 1).toList(),
      ),
    );
  }
  await controller.initialize();
  runApp(NexDropApp(controller: controller));
}

class NexDropApp extends StatelessWidget {
  const NexDropApp({super.key, required this.controller});

  final AppController controller;

  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'NexDrop',
    debugShowCheckedModeBanner: false,
    theme: ThemeData(
      colorScheme: ColorScheme.fromSeed(
        seedColor: const Color(0xff16b98a),
        brightness: Brightness.light,
      ),
      scaffoldBackgroundColor: const Color(0xfff4f6f8),
      useMaterial3: true,
      cardTheme: const CardThemeData(elevation: 0, margin: EdgeInsets.zero),
      inputDecorationTheme: const InputDecorationTheme(
        border: OutlineInputBorder(),
        filled: true,
        fillColor: Colors.white,
      ),
    ),
    home: DesktopLifecycle(
      controller: controller,
      child: AnimatedBuilder(
        animation: controller,
        builder: (context, _) {
          if (controller.loading) {
            return const _LoadingView();
          }
          if (controller.account == null) {
            return LoginView(controller: controller);
          }
          return Workspace(controller: controller);
        },
      ),
    ),
  );
}

class LoginView extends StatefulWidget {
  const LoginView({super.key, required this.controller});
  final AppController controller;

  @override
  State<LoginView> createState() => _LoginViewState();
}

class _LoginViewState extends State<LoginView> {
  final node = TextEditingController(text: 'http://');
  final nodeSecret = TextEditingController();
  final identifier = TextEditingController();
  final password = TextEditingController();
  final totp = TextEditingController();

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _applyPendingJoin());
  }

  @override
  void didUpdateWidget(covariant LoginView oldWidget) {
    super.didUpdateWidget(oldWidget);
    WidgetsBinding.instance.addPostFrameCallback((_) => _applyPendingJoin());
  }

  void _applyPendingJoin() {
    final join = widget.controller.takePendingJoin();
    if (join == null || !mounted) return;
    setState(() {
      node.text = join.nodeUrl;
      nodeSecret.text = join.nodeSecret;
    });
  }

  Future<void> _pasteJoin() async {
    final data = await Clipboard.getData(Clipboard.kTextPlain);
    final join = NodeJoinConfiguration.tryParse(data?.text ?? '');
    if (join == null) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('剪貼簿沒有有效的 NexDrop 導入資料')));
      }
      return;
    }
    widget.controller.queueJoin(join);
  }

  @override
  void dispose() {
    node.dispose();
    nodeSecret.dispose();
    identifier.dispose();
    password.dispose();
    totp.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    body: SafeArea(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(24),
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 440),
            child: Card(
              child: Padding(
                padding: const EdgeInsets.all(32),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    const _Brand(large: true),
                    const SizedBox(height: 28),
                    TextField(
                      controller: node,
                      keyboardType: TextInputType.url,
                      decoration: const InputDecoration(
                        labelText: '節點網址',
                        hintText: 'https://drop.example.com',
                      ),
                    ),
                    const SizedBox(height: 14),
                    TextField(
                      controller: nodeSecret,
                      obscureText: true,
                      decoration: const InputDecoration(labelText: '節點密鑰'),
                    ),
                    const SizedBox(height: 8),
                    Align(
                      alignment: Alignment.centerRight,
                      child: TextButton.icon(
                        onPressed: _pasteJoin,
                        icon: const Icon(Icons.content_paste_go_rounded),
                        label: const Text('從剪貼簿一鍵帶入'),
                      ),
                    ),
                    const SizedBox(height: 6),
                    TextField(
                      controller: identifier,
                      decoration: const InputDecoration(labelText: '帳號或電子郵件'),
                    ),
                    const SizedBox(height: 14),
                    TextField(
                      controller: password,
                      obscureText: true,
                      onSubmitted: (_) => _login(),
                      decoration: const InputDecoration(labelText: '密碼'),
                    ),
                    const SizedBox(height: 14),
                    TextField(
                      controller: totp,
                      keyboardType: TextInputType.number,
                      maxLength: 6,
                      inputFormatters: [FilteringTextInputFormatter.digitsOnly],
                      decoration: const InputDecoration(
                        labelText: 'OTP 六位數驗證碼',
                      ),
                    ),
                    if (widget.controller.error != null)
                      Padding(
                        padding: const EdgeInsets.only(top: 12),
                        child: Text(
                          widget.controller.error!,
                          style: TextStyle(
                            color: Theme.of(context).colorScheme.error,
                          ),
                        ),
                      ),
                    const SizedBox(height: 20),
                    FilledButton(
                      onPressed: widget.controller.busy ? null : _login,
                      child: Text(
                        widget.controller.busy ? '正在安全登入…' : '登入 NexDrop',
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    ),
  );

  void _login() => unawaited(
    widget.controller
        .login(
          node.text,
          nodeSecret.text,
          identifier.text,
          password.text,
          totp.text,
        )
        .catchError((_) {}),
  );
}

class Workspace extends StatefulWidget {
  const Workspace({super.key, required this.controller});
  final AppController controller;

  @override
  State<Workspace> createState() => _WorkspaceState();
}

class _WorkspaceState extends State<Workspace> {
  int selected = 0;
  static const labels = ['聊天室', '傳輸紀錄', '設備', '統計', '設定'];
  static const icons = [
    Icons.send_rounded,
    Icons.history_rounded,
    Icons.devices_rounded,
    Icons.query_stats_rounded,
    Icons.settings_rounded,
  ];

  @override
  Widget build(BuildContext context) {
    final wide = MediaQuery.sizeOf(context).width >= 760;
    final pages = [
      SendView(controller: widget.controller),
      ActivityView(controller: widget.controller),
      DevicesView(controller: widget.controller),
      StatisticsView(controller: widget.controller),
      SettingsView(controller: widget.controller),
    ];
    final body = Column(
      children: [
        if (widget.controller.error != null)
          MaterialBanner(
            content: Text(widget.controller.error!),
            actions: [
              TextButton(
                onPressed: widget.controller.clearError,
                child: const Text('關閉'),
              ),
            ],
          ),
        Expanded(child: pages[selected]),
      ],
    );
    return Scaffold(
      body: wide
          ? Row(
              children: [
                NavigationRail(
                  selectedIndex: selected,
                  onDestinationSelected: (value) =>
                      setState(() => selected = value),
                  extended: MediaQuery.sizeOf(context).width >= 1040,
                  leading: const Padding(
                    padding: EdgeInsets.symmetric(vertical: 20),
                    child: _Brand(),
                  ),
                  destinations: List.generate(
                    labels.length,
                    (index) => NavigationRailDestination(
                      icon: Icon(icons[index]),
                      label: Text(labels[index]),
                    ),
                  ),
                ),
                const VerticalDivider(width: 1),
                Expanded(child: body),
              ],
            )
          : body,
      bottomNavigationBar: wide
          ? null
          : NavigationBar(
              selectedIndex: selected,
              onDestinationSelected: (value) =>
                  setState(() => selected = value),
              destinations: List.generate(
                labels.length,
                (index) => NavigationDestination(
                  icon: Icon(icons[index]),
                  label: labels[index],
                ),
              ),
            ),
    );
  }
}

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
      child: _Page(
        title: '安全傳送',
        subtitle: '區網可用時直接傳輸，否則交由你的 Linux 節點接力。',
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

class ActivityView extends StatefulWidget {
  const ActivityView({super.key, required this.controller});
  final AppController controller;
  @override
  State<ActivityView> createState() => _ActivityViewState();
}

class _ActivityViewState extends State<ActivityView> {
  String? busyId;

  bool _isActive(TransferSummary transfer) => !{
    'DELIVERED',
    'READ',
    'FAILED',
    'CANCELLED',
    'EXPIRED',
  }.contains(transfer.status);

  String _displayStatus(TransferSummary transfer) {
    if (transfer.batchId == null) return transfer.status;
    final children = widget.controller.transfers.where(
      (item) => item.batchId == transfer.batchId,
    );
    if (children.any((item) => item.status == 'FAILED')) return 'FAILED';
    if (children.every(
      (item) => item.status == 'DELIVERED' || item.status == 'READ',
    )) {
      return 'DELIVERED';
    }
    if (children.any((item) => item.status == 'WAITING_FOR_LAN')) {
      return 'PARTIAL_WAITING_FOR_LAN';
    }
    return 'QUEUED';
  }

  @override
  Widget build(BuildContext context) => _Page(
    title: '傳輸紀錄',
    subtitle: '跨區網與節點的任務、路徑與交付狀態。',
    child: Card(
      child: Column(
        children: [
          ...widget.controller.transfers.map(
            (transfer) => ListTile(
              leading: CircleAvatar(
                child: Icon(
                  transfer.files.isEmpty
                      ? Icons.text_snippet_rounded
                      : Icons.insert_drive_file_rounded,
                ),
              ),
              title: Text(
                transfer.files.isEmpty
                    ? transfer.contentType
                    : '${transfer.files.length} 個加密檔案',
              ),
              subtitle: Text(
                '${transfer.targets.map((target) => target.route).join('、')} · ${_date(transfer.createdAt)}${transfer.batchId == null ? '' : ' · 批次 ${transfer.batchId!.substring(0, 8)}'}',
              ),
              onTap:
                  transfer.wrappedContentKeys.containsKey(
                    widget.controller.currentDevice?.id,
                  )
                  ? () => _open(transfer)
                  : null,
              trailing: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  if (busyId == transfer.id)
                    const SizedBox.square(
                      dimension: 20,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  else
                    Chip(label: Text(_displayStatus(transfer))),
                  if (transfer.senderDeviceId ==
                          widget.controller.currentDevice?.id &&
                      _isActive(transfer))
                    IconButton(
                      tooltip: transfer.status == 'PAUSED' ? '繼續傳輸' : '暫停傳輸',
                      icon: Icon(
                        transfer.status == 'PAUSED'
                            ? Icons.play_arrow_rounded
                            : Icons.pause_rounded,
                      ),
                      onPressed: () => unawaited(
                        widget.controller.setTransferPaused(
                          transfer,
                          transfer.status != 'PAUSED',
                        ),
                      ),
                    ),
                  if (transfer.senderDeviceId ==
                          widget.controller.currentDevice?.id &&
                      _isActive(transfer))
                    IconButton(
                      tooltip: '取消傳輸',
                      icon: const Icon(Icons.cancel_outlined),
                      onPressed: () => _cancel(transfer),
                    ),
                  IconButton(
                    tooltip: '刪除訊息紀錄',
                    icon: const Icon(Icons.delete_outline_rounded),
                    onPressed: () => _hide(transfer),
                  ),
                ],
              ),
            ),
          ),
          ...widget.controller.waitingLanTasks.map(
            (task) => ListTile(
              leading: const CircleAvatar(
                child: Icon(Icons.wifi_tethering_rounded),
              ),
              title: Text(File(task.sourcePath).uri.pathSegments.last),
              subtitle: Text('等待區網 · ${task.status}'),
              trailing: Wrap(
                spacing: 4,
                children: [
                  IconButton(
                    tooltip: task.status == 'PAUSED' ? '繼續' : '暫停',
                    icon: Icon(
                      task.status == 'PAUSED'
                          ? Icons.play_arrow_rounded
                          : Icons.pause_rounded,
                    ),
                    onPressed: () => unawaited(
                      widget.controller
                          .setWaitingPaused(task, task.status != 'PAUSED')
                          .catchError((_) {}),
                    ),
                  ),
                  IconButton(
                    tooltip: '重新指定來源檔案',
                    icon: const Icon(Icons.drive_file_move_outline),
                    onPressed: () => _replaceSource(task),
                  ),
                  IconButton(
                    tooltip: '移除等待任務',
                    icon: const Icon(Icons.delete_outline_rounded),
                    onPressed: () => unawaited(
                      widget.controller
                          .removeWaitingTask(task)
                          .catchError((_) {}),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    ),
  );

  Future<void> _open(TransferSummary transfer) async {
    if (busyId != null) return;
    setState(() => busyId = transfer.id);
    try {
      if (transfer.files.isEmpty) {
        final text = await widget.controller.readText(transfer);
        if (!mounted) return;
        await showDialog<void>(
          context: context,
          builder: (context) => AlertDialog(
            title: Text(transfer.contentType == 'URL' ? '網址' : '文字'),
            content: SelectableText(text),
            actions: [
              TextButton.icon(
                onPressed: () async {
                  await Clipboard.setData(ClipboardData(text: text));
                  if (context.mounted) {
                    ScaffoldMessenger.of(
                      context,
                    ).showSnackBar(const SnackBar(content: Text('已複製訊息')));
                  }
                },
                icon: const Icon(Icons.copy_rounded),
                label: const Text('快速複製'),
              ),
              TextButton(
                onPressed: () => Navigator.pop(context),
                child: const Text('關閉'),
              ),
            ],
          ),
        );
      } else {
        final paths = await widget.controller.receiveFiles(transfer);
        if (!mounted) return;
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('已儲存 ${paths.length} 個檔案至 Downloads/NexDrop')),
        );
      }
    } catch (_) {
      // AppController displays the actionable error banner.
    } finally {
      if (mounted) setState(() => busyId = null);
    }
  }

  Future<void> _cancel(TransferSummary transfer) async {
    if (busyId != null) return;
    setState(() => busyId = transfer.id);
    try {
      await widget.controller.cancelTransfer(transfer);
    } catch (_) {
      // AppController displays the actionable error banner.
    } finally {
      if (mounted) setState(() => busyId = null);
    }
  }

  Future<void> _hide(TransferSummary transfer) async {
    if (busyId != null) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('刪除訊息紀錄'),
        content: const Text('只會從你的紀錄移除；其他設備已保存的副本不會被刪除。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('刪除'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    setState(() => busyId = transfer.id);
    try {
      await widget.controller.hideTransfer(transfer);
    } catch (_) {
      // AppController displays the actionable error banner.
    } finally {
      if (mounted) setState(() => busyId = null);
    }
  }

  Future<void> _replaceSource(WaitingLanTask task) async {
    final result = await FilePicker.pickFiles(allowMultiple: false);
    final selected = result?.files.single.path;
    if (selected == null) return;
    try {
      await widget.controller.replaceWaitingSource(task, selected);
    } catch (_) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('檔案內容已變更，請建立新的傳輸任務')));
    }
  }
}

class DevicesView extends StatelessWidget {
  const DevicesView({super.key, required this.controller});
  final AppController controller;

  @override
  Widget build(BuildContext context) => _Page(
    title: '設備',
    subtitle: '新設備開啟導入連結後，會自動帶入節點網址與節點密鑰。',
    child: Card(
      child: controller.devices.isEmpty
          ? const Padding(
              padding: EdgeInsets.all(32),
              child: Center(child: Text('尚未加入任何設備')),
            )
          : Column(
              children: controller.devices
                  .map(
                    (device) => ListTile(
                      leading: Icon(
                        device.type == 'ANDROID'
                            ? Icons.phone_android_rounded
                            : Icons.computer_rounded,
                      ),
                      title: Text(device.displayName),
                      subtitle: Text(
                        '${device.type} · ${device.online
                            ? '在線'
                            : device.lastSeenAt == null
                            ? '尚未連線'
                            : '最後上線 ${_date(device.lastSeenAt!)}'}${device.lanCapable ? ' · LAN' : ''}',
                      ),
                      trailing: device.trustStatus == 'REVOKED'
                          ? const Chip(label: Text('已撤銷'))
                          : const Icon(Icons.verified_user_rounded),
                    ),
                  )
                  .toList(),
            ),
    ),
  );
}

class StatisticsView extends StatelessWidget {
  const StatisticsView({super.key, required this.controller});
  final AppController controller;
  @override
  Widget build(BuildContext context) => _Page(
    title: '設備與傳輸統計',
    subtitle: controller.nodeOnline ? 'Linux 節點在線，狀態每 5 秒更新。' : 'Linux 節點目前離線。',
    child: Card(
      child: Column(
        children: controller.deviceStatistics
            .map(
              (item) => ListTile(
                leading: CircleAvatar(
                  backgroundColor: item.online
                      ? Colors.green.shade100
                      : Colors.grey.shade200,
                  child: Icon(
                    item.online ? Icons.wifi_rounded : Icons.wifi_off_rounded,
                  ),
                ),
                title: Text(item.displayName),
                subtitle: Text(
                  '傳送 ${item.sentCount} 筆／${_formatBytes(item.sentBytes)} · '
                  '接收 ${item.receivedCount} 筆／${_formatBytes(item.receivedBytes)}\n'
                  '${item.online
                      ? '目前在線'
                      : item.lastSeenAt == null
                      ? '尚未連線'
                      : '最後上線 ${_date(item.lastSeenAt!)}'}',
                ),
                isThreeLine: true,
                trailing: Text(item.deviceType.replaceFirst('WEB_', '')),
              ),
            )
            .toList(),
      ),
    ),
  );
}

String _formatBytes(int value) {
  if (value < 1024) return '$value B';
  if (value < 1024 * 1024) return '${(value / 1024).toStringAsFixed(1)} KB';
  if (value < 1024 * 1024 * 1024) {
    return '${(value / (1024 * 1024)).toStringAsFixed(1)} MB';
  }
  return '${(value / (1024 * 1024 * 1024)).toStringAsFixed(1)} GB';
}

class SettingsView extends StatelessWidget {
  const SettingsView({super.key, required this.controller});
  final AppController controller;
  @override
  Widget build(BuildContext context) => _Page(
    title: '設定',
    subtitle: '本機私鑰保存在平台安全儲存，不會同步到節點。',
    child: Card(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.dns_rounded),
              title: Text(controller.api.node.toString()),
              subtitle: Text(controller.nodeOnline ? '節點在線' : '節點離線'),
            ),
            if (controller.api.nodeJoinUri case final joinUri?)
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.link_rounded),
                title: const Text('其他設備一鍵加入'),
                subtitle: const Text('複製後在另一台 Windows 或 Android 設備開啟。'),
                trailing: const Icon(Icons.copy_rounded),
                onTap: () async {
                  await Clipboard.setData(ClipboardData(text: joinUri));
                  if (context.mounted) {
                    ScaffoldMessenger.of(
                      context,
                    ).showSnackBar(const SnackBar(content: Text('已複製一鍵加入連結')));
                  }
                },
              ),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              secondary: const Icon(Icons.cloud_upload_rounded),
              title: const Text('允許透過非區域網路傳送大檔案'),
              subtitle: const Text('預設關閉；單一檔案超過 100 MB 時適用。'),
              value: controller.allowLargeFileViaNode,
              onChanged: (value) => unawaited(
                controller.setAllowLargeFileViaNode(value).catchError((_) {}),
              ),
            ),
            if (Platform.isWindows)
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.folder_rounded),
                title: const Text('接收檔案位置'),
                subtitle: Text(
                  controller.receiveDirectory ?? 'Downloads\\NexDrop',
                ),
                trailing: const Icon(Icons.edit_outlined),
                onTap: () async {
                  final selected = await FilePicker.getDirectoryPath(
                    dialogTitle: '選擇 NexDrop 接收位置',
                  );
                  if (selected != null) {
                    await controller.setReceiveDirectory(selected);
                  }
                },
              ),
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.person_rounded),
              title: Text(controller.account!.username),
              subtitle: Text(controller.account!.email),
            ),
            const SizedBox(height: 12),
            OutlinedButton.icon(
              onPressed: controller.logout,
              icon: const Icon(Icons.logout_rounded),
              label: const Text('登出'),
            ),
          ],
        ),
      ),
    ),
  );
}

class _Page extends StatelessWidget {
  const _Page({
    required this.title,
    required this.subtitle,
    required this.child,
  });
  final String title;
  final String subtitle;
  final Widget child;
  @override
  Widget build(BuildContext context) => RefreshIndicator(
    onRefresh: () =>
        (context
            .findAncestorWidgetOfExactType<Workspace>()
            ?.controller
            .reload() ??
        Future.value()),
    child: ListView(
      padding: const EdgeInsets.all(28),
      children: [
        Text(
          title,
          style: Theme.of(
            context,
          ).textTheme.headlineMedium?.copyWith(fontWeight: FontWeight.w800),
        ),
        const SizedBox(height: 4),
        Text(
          subtitle,
          style: Theme.of(
            context,
          ).textTheme.bodyLarge?.copyWith(color: Colors.blueGrey),
        ),
        const SizedBox(height: 24),
        child,
      ],
    ),
  );
}

class _Brand extends StatelessWidget {
  const _Brand({this.large = false});
  final bool large;
  @override
  Widget build(BuildContext context) => Row(
    mainAxisSize: MainAxisSize.min,
    children: [
      Container(
        width: large ? 44 : 34,
        height: large ? 44 : 34,
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.primary,
          borderRadius: BorderRadius.circular(10),
        ),
        child: const Icon(Icons.north_east_rounded, color: Colors.white),
      ),
      const SizedBox(width: 10),
      Text(
        'NexDrop',
        style:
            (large
                    ? Theme.of(context).textTheme.headlineSmall
                    : Theme.of(context).textTheme.titleMedium)
                ?.copyWith(fontWeight: FontWeight.w900),
      ),
    ],
  );
}

class _LoadingView extends StatelessWidget {
  const _LoadingView();
  @override
  Widget build(BuildContext context) =>
      const Scaffold(body: Center(child: CircularProgressIndicator()));
}

String _date(DateTime value) =>
    '${value.month.toString().padLeft(2, '0')}/${value.day.toString().padLeft(2, '0')} ${value.hour.toString().padLeft(2, '0')}:${value.minute.toString().padLeft(2, '0')}';
