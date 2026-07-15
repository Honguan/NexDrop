import 'dart:async';
import 'dart:io';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:tray_manager/tray_manager.dart';
import 'package:window_manager/window_manager.dart';
import 'package:workmanager/workmanager.dart';

import 'app_controller.dart';
import 'core/api_client.dart';
import 'core/local_database.dart';
import 'core/platform_share.dart';

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
    home: AnimatedBuilder(
      animation: controller,
      builder: (context, _) {
        if (controller.loading) {
          return const _LoadingView();
        }
        if (controller.account == null) {
          return LoginView(controller: controller);
        }
        return DesktopLifecycle(
          controller: controller,
          child: Workspace(controller: controller),
        );
      },
    ),
  );
}

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
  @override
  void initState() {
    super.initState();
    if (Platform.isWindows) unawaited(_initializeDesktop());
  }

  Future<void> _initializeDesktop() async {
    windowManager.addListener(this);
    trayManager.addListener(this);
    await trayManager.setIcon('windows/runner/resources/app_icon.ico');
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
  void onWindowClose() => unawaited(windowManager.hide());

  @override
  void onTrayIconMouseDown() => unawaited(windowManager.show());

  @override
  void onTrayMenuItemClick(MenuItem menuItem) {
    if (menuItem.key == 'show') unawaited(windowManager.show());
    if (menuItem.key == 'exit') {
      unawaited(
        windowManager.setPreventClose(false).then((_) => windowManager.close()),
      );
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

class LoginView extends StatefulWidget {
  const LoginView({super.key, required this.controller});
  final AppController controller;

  @override
  State<LoginView> createState() => _LoginViewState();
}

class _LoginViewState extends State<LoginView> {
  final node = TextEditingController(text: 'https://');
  final identifier = TextEditingController();
  final password = TextEditingController();

  @override
  Widget build(BuildContext context) => Scaffold(
    body: Center(
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
  );

  void _login() => unawaited(
    widget.controller
        .login(node.text, identifier.text, password.text)
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
  static const labels = ['傳送', '傳輸紀錄', '設備', '群組', '設定'];
  static const icons = [
    Icons.send_rounded,
    Icons.history_rounded,
    Icons.devices_rounded,
    Icons.group_work_rounded,
    Icons.settings_rounded,
  ];

  @override
  Widget build(BuildContext context) {
    final wide = MediaQuery.sizeOf(context).width >= 760;
    final pages = [
      SendView(controller: widget.controller),
      ActivityView(controller: widget.controller),
      DevicesView(controller: widget.controller),
      GroupsView(controller: widget.controller),
      SettingsView(controller: widget.controller),
    ];
    final body = Column(
      children: [
        if (widget.controller.currentDevice?.trusted != true)
          _PendingBanner(controller: widget.controller),
        if (widget.controller.error != null)
          MaterialBanner(
            content: Text(widget.controller.error!),
            actions: [TextButton(onPressed: () {}, child: const Text('關閉'))],
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
  String? groupId;
  List<String> files = const [];

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _applySharedContent());
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
    return _Page(
      title: '安全傳送',
      subtitle: '區網可用時直接傳輸，否則交由你的 Linux 節點接力。',
      child: Card(
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
                decoration: const InputDecoration(
                  labelText: '文字或網址',
                  alignLabelWithHint: true,
                ),
              ),
              const SizedBox(height: 12),
              OutlinedButton.icon(
                onPressed: _pickFiles,
                icon: const Icon(Icons.attach_file_rounded),
                label: Text(files.isEmpty ? '選擇檔案' : '${files.length} 個檔案已選擇'),
              ),
              const SizedBox(height: 22),
              Text('信任設備', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 8),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: trusted
                    .map(
                      (device) => FilterChip(
                        label: Text(device.displayName),
                        selected: selectedDevices.contains(device.id),
                        onSelected: groupId == null
                            ? (_) => setState(
                                () => selectedDevices.contains(device.id)
                                    ? selectedDevices.remove(device.id)
                                    : selectedDevices.add(device.id),
                              )
                            : null,
                      ),
                    )
                    .toList(),
              ),
              if (widget.controller.groups.isNotEmpty) ...[
                const SizedBox(height: 18),
                DropdownButtonFormField<String?>(
                  initialValue: groupId,
                  decoration: const InputDecoration(labelText: '或傳送至群組'),
                  items: [
                    const DropdownMenuItem(value: null, child: Text('不使用群組')),
                    ...widget.controller.groups.map(
                      (group) => DropdownMenuItem(
                        value: group.id,
                        child: Text(group.name),
                      ),
                    ),
                  ],
                  onChanged: (value) => setState(() {
                    groupId = value;
                    if (value != null) selectedDevices.clear();
                  }),
                ),
              ],
              const SizedBox(height: 24),
              FilledButton.icon(
                onPressed: _canSend && !widget.controller.busy ? _send : null,
                icon: const Icon(Icons.lock_rounded),
                label: Text(widget.controller.busy ? '加密與傳送中…' : '建立安全傳輸'),
              ),
            ],
          ),
        ),
      ),
    );
  }

  bool get _canSend =>
      (content.text.trim().isNotEmpty || files.isNotEmpty) &&
      (groupId != null || selectedDevices.isNotEmpty) &&
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

  void _send() {
    final recipients = widget.controller.devices
        .where((device) => selectedDevices.contains(device.id))
        .toList();
    unawaited(
      widget.controller
          .send(
            content: content.text,
            recipients: recipients,
            groupId: groupId,
            files: files,
          )
          .then((_) {
            if (!mounted) return;
            setState(() {
              content.clear();
              files = const [];
              selectedDevices.clear();
              groupId = null;
            });
            ScaffoldMessenger.of(
              context,
            ).showSnackBar(const SnackBar(content: Text('已建立安全傳輸')));
          })
          .catchError((_) {}),
    );
  }
}

class ActivityView extends StatelessWidget {
  const ActivityView({super.key, required this.controller});
  final AppController controller;
  @override
  Widget build(BuildContext context) => _Page(
    title: '傳輸紀錄',
    subtitle: '跨區網與節點的任務、路徑與交付狀態。',
    child: Card(
      child: Column(
        children: controller.transfers
            .map(
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
                  '${transfer.targets.map((target) => target.route).join('、')} · ${_date(transfer.createdAt)}',
                ),
                trailing: Chip(label: Text(transfer.status)),
              ),
            )
            .toList(),
      ),
    ),
  );
}

class DevicesView extends StatelessWidget {
  const DevicesView({super.key, required this.controller});
  final AppController controller;
  @override
  Widget build(BuildContext context) => _Page(
    title: '設備',
    subtitle: '只有核准設備能解密內容與建立傳輸。',
    child: Card(
      child: Column(
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
                  '${device.type} · ${device.trustStatus}${device.lanCapable ? ' · LAN' : ''}',
                ),
                trailing:
                    device.trustStatus == 'PENDING' &&
                        controller.account?.admin == true
                    ? FilledButton.tonal(
                        onPressed: () => unawaited(
                          controller.approve(device).catchError((_) {}),
                        ),
                        child: const Text('核准'),
                      )
                    : const Icon(Icons.verified_user_rounded),
              ),
            )
            .toList(),
      ),
    ),
  );
}

class GroupsView extends StatelessWidget {
  const GroupsView({super.key, required this.controller});
  final AppController controller;
  @override
  Widget build(BuildContext context) => _Page(
    title: '群組',
    subtitle: '固定協作對象可一次傳送到所有群組設備。',
    child: Wrap(
      spacing: 12,
      runSpacing: 12,
      children: controller.groups
          .map(
            (group) => SizedBox(
              width: 280,
              child: Card(
                child: ListTile(
                  leading: const Icon(Icons.group_work_rounded),
                  title: Text(group.name),
                  subtitle: Text(group.role),
                ),
              ),
            ),
          )
          .toList(),
    ),
  );
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

class _PendingBanner extends StatelessWidget {
  const _PendingBanner({required this.controller});
  final AppController controller;
  @override
  Widget build(BuildContext context) => MaterialBanner(
    leading: const Icon(Icons.phonelink_lock_rounded),
    content: const Text('此設備尚待核准；可由管理員核准，或輸入另一台信任設備產生的配對資料。'),
    actions: [
      TextButton(
        onPressed: () => _showPairDialog(context, controller),
        child: const Text('輸入配對碼'),
      ),
    ],
  );
}

Future<void> _showPairDialog(
  BuildContext context,
  AppController controller,
) async {
  final challenge = TextEditingController();
  final code = TextEditingController();
  await showDialog<void>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('配對此設備'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextField(
            controller: challenge,
            decoration: const InputDecoration(labelText: '挑戰 ID'),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: code,
            keyboardType: TextInputType.number,
            decoration: const InputDecoration(labelText: '6 位數配對碼'),
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: () {
            Navigator.pop(context);
            unawaited(
              controller
                  .redeemPairingCode(challenge.text, code.text)
                  .catchError((_) {}),
            );
          },
          child: const Text('配對'),
        ),
      ],
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
