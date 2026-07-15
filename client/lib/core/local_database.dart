import 'dart:io';

import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';
import 'package:sqflite/sqflite.dart' as mobile;
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

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
        version: 1,
        onConfigure: (database) => database.execute('PRAGMA foreign_keys = ON'),
        onCreate: _create,
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
        target_device_id TEXT NOT NULL,
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
  }

  Future<void> saveSetting(String key, String value) async {
    final database = await open();
    await database.insert('settings', {
      'key': key,
      'value': value,
      'updated_at': DateTime.now().toUtc().toIso8601String(),
    }, conflictAlgorithm: ConflictAlgorithm.replace);
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

  Future<void> close() async {
    await _database?.close();
    _database = null;
  }
}
