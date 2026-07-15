import 'dart:async';

import 'package:flutter/services.dart';

class PlatformSharePayload {
  const PlatformSharePayload({required this.text, required this.files});

  factory PlatformSharePayload.fromMap(Map<dynamic, dynamic> value) =>
      PlatformSharePayload(
        text: value['text'] as String? ?? '',
        files: (value['files'] as List<dynamic>? ?? const []).cast<String>(),
      );

  final String text;
  final List<String> files;
}

class PlatformShareService {
  static const _methods = MethodChannel('com.nexdrop/client');
  static const _events = EventChannel('com.nexdrop/client/shares');
  final _shares = StreamController<PlatformSharePayload>.broadcast();
  StreamSubscription<dynamic>? _subscription;

  Stream<PlatformSharePayload> get shares => _shares.stream;

  Future<void> startTransferService() => _optionalCall('startTransferService');

  Future<void> stopTransferService() => _optionalCall('stopTransferService');

  Future<void> _optionalCall(String method) async {
    try {
      await _methods.invokeMethod<void>(method);
    } on MissingPluginException {
      // Only Android provides the foreground transfer service.
    } on PlatformException {
      // The transfer itself can continue if Android rejects the service start.
    }
  }

  Future<void> initialize() async {
    try {
      final initial = await _methods.invokeMapMethod<dynamic, dynamic>(
        'getInitialShare',
      );
      if (initial != null) _shares.add(PlatformSharePayload.fromMap(initial));
      _subscription = _events.receiveBroadcastStream().listen((value) {
        if (value is Map<dynamic, dynamic>) {
          _shares.add(PlatformSharePayload.fromMap(value));
        }
      }, onError: (_) {});
    } on MissingPluginException {
      // Desktop platforms use command-line integration instead.
    } on PlatformException {
      // Sharing remains optional when the host platform rejects an item.
    }
  }

  Future<void> dispose() async {
    await _subscription?.cancel();
    await _shares.close();
  }
}
