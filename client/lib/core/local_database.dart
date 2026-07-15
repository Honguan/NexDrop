import 'dart:convert';
import 'dart:io';

import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';
import 'package:sqflite/sqflite.dart' as mobile;
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

import 'models.dart';

class WaitingLanTask {
  const WaitingLanTask({
    required this.id,
    required this.transferId,
    required this.fileId,
    required this.fileIndex,
    required this.targetDeviceId,
    required this.targetRoute,
    required this.sourcePath,
    required this.sourceSize,
    required this.sourceModifiedAt,
    required this.sourceSha256,
    required this.status,
  });

  factory WaitingLanTask.fromRow(Map<String, Object?> row) => WaitingLanTask(
    id: row['id'] as String,
    transferId: row['transfer_id'] as String,
    fileId: row['file_id'] as String,
    fileIndex: row['file_index'] as int,
    targetDeviceId: row['target_device_id'] as String,
    targetRoute: row['target_route'] as String,
    sourcePath: row['source_path'] as String,
    sourceSize: row['source_size'] as int,
    sourceModifiedAt: DateTime.parse(row['source_modified_at'] as String),
    sourceSha256: row['source_sha256'] as String,
    status: row['status'] as String,
  );

  final String id;
  final String transferId;
  final String fileId;
  final int fileIndex;
  final String targetDeviceId;
  final String targetRoute;
  final String sourcePath;
  final int sourceSize;
  final DateTime sourceModifiedAt;
  final String sourceSha256;
  final String status;
}

class LocalDraft {
  const LocalDraft({required this.id, required this.payload});

  final String id;
  final Map<String, dynamic> payload;
}

class LocalDatabase {
  Database? _database;

  Future<Database> open() async {
    if (_database != null) return _database!;
    if (Platform.isWindows) {
      sqfliteFfiInit();
      databaseFactory = databaseFactoryFfi;
    } else {
      databaseFactory = mobile.databaseFactorySqflitePlugin;
    }
    final support = await getApplicationSupportDirectory();
    await support.create(recursive: true);
    _database = await databaseFactory.openDatabase(
      path.join(support.path, 'nexdrop.db'),
      options: OpenDatabaseOptions(
        version: 4,
        onConfigure: (database) => database.execute('PRAGMA foreign_keys = ON'),
        onCreate: _create,
        onUpgrade: _upgrade,
      ),
    );
    return _database!;
  }

  Future<void> _create(Database database, int version) async {
    await database.execute('''
      CREATE TABLE settings (
        key TEXT PRIMARY KEY,
        value TEXT NOT NULL,
        updated_at TEXT NOT NULL
      )
    ''');
    await database.execute('''
      CREATE TABLE transfer_history (
        transfer_id TEXT PRIMARY KEY,
        content_type TEXT NOT NULL,
        route TEXT NOT NULL,
        status TEXT NOT NULL,
        total_bytes INTEGER NOT NULL DEFAULT 0,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL
      )
    ''');
    await database.execute('''
      CREATE TABLE drafts (
        id TEXT PRIMARY KEY,
        encrypted_payload TEXT NOT NULL,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL
      )
    ''');
    await database.execute('''
      CREATE TABLE waiting_lan_tasks (
        id TEXT PRIMARY KEY,
        transfer_id TEXT NOT NULL,
        file_id TEXT NOT NULL,
        file_index INTEGER NOT NULL,
        target_device_id TEXT NOT NULL,
        target_route TEXT NOT NULL,
        source_path TEXT NOT NULL,
        source_size INTEGER NOT NULL,
        source_modified_at TEXT NOT NULL,
        source_sha256 TEXT NOT NULL,
        completed_chunks TEXT NOT NULL DEFAULT '[]',
        status TEXT NOT NULL,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL
      )
    ''');
    await database.execute('''
      CREATE TABLE pending_metrics (
        event_id TEXT PRIMARY KEY,
        payload TEXT NOT NULL,
        created_at TEXT NOT NULL
      )
    ''');
    await database.execute('''
      CREATE TABLE downloads (
        file_id TEXT PRIMARY KEY,
        local_path TEXT NOT NULL,
        downloaded_at TEXT NOT NULL
      )
    ''');
    await database.execute('''
      CREATE TABLE read_states (
        transfer_id TEXT PRIMARY KEY,
        read_at TEXT NOT NULL
      )
    ''');
    await database.execute('''
      CREATE TABLE local_transfers (
        transfer_id TEXT PRIMARY KEY,
        payload TEXT NOT NULL,
        updated_at TEXT NOT NULL
      )
    ''');
  }

  Future<void> _upgrade(
    Database database,
    int oldVersion,
    int newVersion,
  ) async {
    if (oldVersion < 2) {
      await database.execute(
        "ALTER TABLE waiting_lan_tasks ADD COLUMN transfer_id TEXT NOT NULL DEFAULT ''",
      );
      await database.execute(
        "ALTER TABLE waiting_lan_tasks ADD COLUMN file_id TEXT NOT NULL DEFAULT ''",
      );
      await database.execute(
        'ALTER TABLE waiting_lan_tasks ADD COLUMN file_index INTEGER NOT NULL DEFAULT 0',
      );
    }
    if (oldVersion < 3) {
      await database.execute(
        "ALTER TABLE waiting_lan_tasks ADD COLUMN target_route TEXT NOT NULL DEFAULT 'WAITING_LAN'",
      );
    }
    if (oldVersion < 4) {
      await database.execute('''
        CREATE TABLE local_transfers (
          transfer_id TEXT PRIMARY KEY,
          payload TEXT NOT NULL,
          updated_at TEXT NOT NULL
        )
      ''');
    }
  }

  Future<void> saveSetting(String key, String value) async {
    final database = await open();
    await database.insert('settings', {
      'key': key,
      'value': value,
      'updated_at': DateTime.now().toUtc().toIso8601String(),
    }, conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<void> saveDraft(String id, Map<String, dynamic> payload) async {
    final database = await open();
    final now = DateTime.now().toUtc().toIso8601String();
    await database.insert('drafts', {
      'id': id,
      'encrypted_payload': jsonEncode(payload),
      'created_at': now,
      'updated_at': now,
    }, conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<List<LocalDraft>> drafts() async {
    final database = await open();
    final rows = await database.query('drafts', orderBy: 'created_at ASC');
    return rows
        .map(
          (row) => LocalDraft(
            id: row['id'] as String,
            payload:
                jsonDecode(row['encrypted_payload'] as String)
                    as Map<String, dynamic>,
          ),
        )
        .toList();
  }

  Future<void> deleteDraft(String id) async {
    final database = await open();
    await database.delete('drafts', where: 'id = ?', whereArgs: [id]);
  }

  Future<void> savePendingMetric(
    String eventId,
    Map<String, dynamic> payload,
  ) async {
    final database = await open();
    await database.insert('pending_metrics', {
      'event_id': eventId,
      'payload': jsonEncode(payload),
      'created_at': DateTime.now().toUtc().toIso8601String(),
    }, conflictAlgorithm: ConflictAlgorithm.ignore);
  }

  Future<List<Map<String, dynamic>>> pendingMetrics({int limit = 500}) async {
    final database = await open();
    final rows = await database.query(
      'pending_metrics',
      orderBy: 'created_at ASC',
      limit: limit,
    );
    return rows
        .map(
          (row) => {
            'eventId': row['event_id'] as String,
            'payload': jsonDecode(row['payload'] as String),
          },
        )
        .toList();
  }

  Future<void> deletePendingMetrics(Iterable<String> eventIds) async {
    final ids = eventIds.toList();
    if (ids.isEmpty) return;
    final database = await open();
    await database.delete(
      'pending_metrics',
      where: 'event_id IN (${List.filled(ids.length, '?').join(',')})',
      whereArgs: ids,
    );
  }

  Future<String?> setting(String key) async {
    final database = await open();
    final rows = await database.query(
      'settings',
      columns: ['value'],
      where: 'key = ?',
      whereArgs: [key],
      limit: 1,
    );
    return rows.isEmpty ? null : rows.first['value'] as String;
  }

  Future<void> cacheTransfer({
    required String id,
    required String contentType,
    required String route,
    required String status,
    required int totalBytes,
    required DateTime createdAt,
  }) async {
    final database = await open();
    final now = DateTime.now().toUtc().toIso8601String();
    await database.insert('transfer_history', {
      'transfer_id': id,
      'content_type': contentType,
      'route': route,
      'status': status,
      'total_bytes': totalBytes,
      'created_at': createdAt.toUtc().toIso8601String(),
      'updated_at': now,
    }, conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<void> cacheLocalTransfer(TransferSummary transfer) async {
    final database = await open();
    final payload = jsonEncode({
      'id': transfer.id,
      'senderDeviceId': transfer.senderDeviceId,
      'contentType': transfer.contentType,
      'content': transfer.encryptedContent,
      'wrappedContentKeys': transfer.wrappedContentKeys,
      'status': transfer.status,
      'createdAt': transfer.createdAt.toUtc().toIso8601String(),
      'targets': [
        for (final target in transfer.targets)
          {
            'deviceId': target.deviceId,
            'selectedRoute': target.route,
            'status': target.status,
            'bytesTransferred': target.bytesTransferred,
          },
      ],
      'files': [
        for (final file in transfer.files)
          {
            'id': file.id,
            'name': file.name,
            'size': file.size,
            'chunkCount': file.chunkCount,
            'chunkSize': file.chunkSize,
            'sha256': file.sha256,
          },
      ],
      'fileTargets': [
        for (final target in transfer.fileTargets)
          {
            'fileIndex': target.fileIndex,
            'deviceId': target.deviceId,
            'selectedRoute': target.route,
            'status': target.status,
          },
      ],
    });
    await database.insert('local_transfers', {
      'transfer_id': transfer.id,
      'payload': payload,
      'updated_at': DateTime.now().toUtc().toIso8601String(),
    }, conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<List<TransferSummary>> localTransfers() async {
    final database = await open();
    final rows = await database.query(
      'local_transfers',
      orderBy: 'updated_at DESC',
    );
    return rows
        .map(
          (row) => TransferSummary.fromJson(
            jsonDecode(row['payload'] as String) as Map<String, dynamic>,
          ),
        )
        .toList();
  }

  Future<void> deleteLocalTransfer(String transferId) async {
    final database = await open();
    await database.delete(
      'local_transfers',
      where: 'transfer_id = ?',
      whereArgs: [transferId],
    );
  }

  Future<void> recordDownload(String fileId, String localPath) async {
    final database = await open();
    await database.insert('downloads', {
      'file_id': fileId,
      'local_path': localPath,
      'downloaded_at': DateTime.now().toUtc().toIso8601String(),
    }, conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<void> saveWaitingLanTask({
    required String id,
    required String transferId,
    required String fileId,
    required int fileIndex,
    required String targetDeviceId,
    required String targetRoute,
    required String sourcePath,
    required int sourceSize,
    required DateTime sourceModifiedAt,
    required String sourceSha256,
  }) async {
    final database = await open();
    final now = DateTime.now().toUtc().toIso8601String();
    await database.insert('waiting_lan_tasks', {
      'id': id,
      'transfer_id': transferId,
      'file_id': fileId,
      'file_index': fileIndex,
      'target_device_id': targetDeviceId,
      'target_route': targetRoute,
      'source_path': sourcePath,
      'source_size': sourceSize,
      'source_modified_at': sourceModifiedAt.toUtc().toIso8601String(),
      'source_sha256': sourceSha256,
      'completed_chunks': '[]',
      'status': 'WAITING_FOR_LAN',
      'created_at': now,
      'updated_at': now,
    }, conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<List<WaitingLanTask>> waitingLanTasks() async {
    final database = await open();
    final rows = await database.query(
      'waiting_lan_tasks',
      where: 'transfer_id <> ? AND file_id <> ?',
      whereArgs: ['', ''],
      orderBy: 'created_at ASC',
    );
    return rows.map(WaitingLanTask.fromRow).toList();
  }

  Future<void> updateWaitingLanStatus(String id, String status) async {
    final database = await open();
    await database.update(
      'waiting_lan_tasks',
      {
        'status': status,
        'updated_at': DateTime.now().toUtc().toIso8601String(),
      },
      where: 'id = ?',
      whereArgs: [id],
    );
  }

  Future<void> deleteWaitingLanTask(String id) async {
    final database = await open();
    await database.delete(
      'waiting_lan_tasks',
      where: 'id = ?',
      whereArgs: [id],
    );
  }

  Future<void> replaceWaitingLanSource({
    required String id,
    required String sourcePath,
    required int sourceSize,
    required DateTime sourceModifiedAt,
    required String sourceSha256,
  }) async {
    final database = await open();
    await database.update(
      'waiting_lan_tasks',
      {
        'source_path': sourcePath,
        'source_size': sourceSize,
        'source_modified_at': sourceModifiedAt.toUtc().toIso8601String(),
        'source_sha256': sourceSha256,
        'status': 'WAITING_FOR_LAN',
        'updated_at': DateTime.now().toUtc().toIso8601String(),
      },
      where: 'id = ?',
      whereArgs: [id],
    );
  }

  Future<void> close() async {
    await _database?.close();
    _database = null;
  }
}
