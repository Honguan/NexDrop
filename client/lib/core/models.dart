class UserAccount {
  const UserAccount({
    required this.id,
    required this.username,
    required this.email,
    required this.admin,
  });

  factory UserAccount.fromJson(Map<String, dynamic> json) => UserAccount(
    id: json['id'] as String,
    username: json['username'] as String,
    email: json['email'] as String,
    admin: json['admin'] as bool? ?? false,
  );

  final String id;
  final String username;
  final String email;
  final bool admin;
}

class Device {
  const Device({
    required this.id,
    required this.displayName,
    required this.type,
    required this.trustStatus,
    this.publicKey,
    this.lanShortId,
    this.lanCertificateFingerprint,
    this.lanCertificate,
    this.online = false,
    this.lastSeenAt,
  });

  factory Device.fromJson(Map<String, dynamic> json) => Device(
    id: json['id'] as String,
    displayName: json['displayName'] as String,
    type: json['type'] as String,
    trustStatus: json['trustStatus'] as String? ?? 'TRUSTED',
    publicKey: json['publicKey'] as String?,
    lanShortId: json['lanShortId'] as String?,
    lanCertificateFingerprint: json['lanCertificateFingerprint'] as String?,
    lanCertificate: json['lanCertificate'] as String?,
    online: json['online'] as bool? ?? false,
    lastSeenAt: json['lastSeenAt'] == null
        ? null
        : DateTime.parse(json['lastSeenAt'] as String).toLocal(),
  );

  final String id;
  final String displayName;
  final String type;
  final String trustStatus;
  final String? publicKey;
  final String? lanShortId;
  final String? lanCertificateFingerprint;
  final String? lanCertificate;
  final bool online;
  final DateTime? lastSeenAt;

  bool get trusted => trustStatus == 'TRUSTED';
  bool get lanCapable =>
      lanShortId != null &&
      lanCertificateFingerprint != null &&
      lanCertificate != null;
}

class DeviceStatistic {
  const DeviceStatistic({
    required this.deviceId,
    required this.displayName,
    required this.deviceType,
    required this.online,
    required this.sentCount,
    required this.receivedCount,
    required this.sentBytes,
    required this.receivedBytes,
    this.lastSeenAt,
  });

  factory DeviceStatistic.fromJson(Map<String, dynamic> json) => DeviceStatistic(
    deviceId: json['deviceId'] as String,
    displayName: json['displayName'] as String,
    deviceType: json['deviceType'] as String,
    online: json['online'] as bool? ?? false,
    sentCount: json['sentCount'] as int? ?? 0,
    receivedCount: json['receivedCount'] as int? ?? 0,
    sentBytes: json['sentBytes'] as int? ?? 0,
    receivedBytes: json['receivedBytes'] as int? ?? 0,
    lastSeenAt: json['lastSeenAt'] == null
        ? null
        : DateTime.parse(json['lastSeenAt'] as String).toLocal(),
  );

  final String deviceId;
  final String displayName;
  final String deviceType;
  final bool online;
  final DateTime? lastSeenAt;
  final int sentCount;
  final int receivedCount;
  final int sentBytes;
  final int receivedBytes;
}

class GroupSummary {
  const GroupSummary({
    required this.id,
    required this.name,
    required this.role,
  });

  factory GroupSummary.fromJson(Map<String, dynamic> json) => GroupSummary(
    id: json['id'] as String,
    name: json['name'] as String,
    role: json['role'] as String,
  );

  final String id;
  final String name;
  final String role;
}

class TransferSummary {
  const TransferSummary({
    required this.id,
    required this.contentType,
    required this.status,
    required this.createdAt,
    required this.targets,
    required this.files,
    this.senderDeviceId,
    this.batchId,
    this.fileTargets = const [],
    this.encryptedContent,
    this.wrappedContentKeys = const {},
  });

  factory TransferSummary.fromJson(
    Map<String, dynamic> json,
  ) => TransferSummary(
    id: json['id'] as String,
    contentType: json['contentType'] as String,
    status: json['status'] as String,
    createdAt: DateTime.parse(json['createdAt'] as String).toLocal(),
    senderDeviceId: json['senderDeviceId'] as String?,
    batchId: json['batchId'] as String?,
    encryptedContent: json['content'] as String?,
    wrappedContentKeys:
        (json['wrappedContentKeys'] as Map<String, dynamic>? ?? {}).map(
          (key, value) => MapEntry(key, value as String),
        ),
    targets: (json['targets'] as List<dynamic>? ?? [])
        .map((value) => TransferTarget.fromJson(value as Map<String, dynamic>))
        .toList(),
    files: (json['files'] as List<dynamic>? ?? [])
        .map((value) => TransferFile.fromJson(value as Map<String, dynamic>))
        .toList(),
    fileTargets: (json['fileTargets'] as List<dynamic>? ?? [])
        .map(
          (value) => TransferFileTarget.fromJson(value as Map<String, dynamic>),
        )
        .toList(),
  );

  final String id;
  final String contentType;
  final String status;
  final DateTime createdAt;
  final String? senderDeviceId;
  final String? batchId;
  final String? encryptedContent;
  final Map<String, String> wrappedContentKeys;
  final List<TransferTarget> targets;
  final List<TransferFile> files;
  final List<TransferFileTarget> fileTargets;
}

class TransferFileTarget {
  const TransferFileTarget({
    required this.fileIndex,
    required this.deviceId,
    required this.route,
    required this.status,
  });

  factory TransferFileTarget.fromJson(Map<String, dynamic> json) =>
      TransferFileTarget(
        fileIndex: json['fileIndex'] as int,
        deviceId: json['deviceId'] as String,
        route: json['selectedRoute'] as String,
        status: json['status'] as String,
      );

  final int fileIndex;
  final String deviceId;
  final String route;
  final String status;
}

class TransferTarget {
  const TransferTarget({
    required this.deviceId,
    required this.route,
    required this.status,
    required this.bytesTransferred,
  });

  factory TransferTarget.fromJson(Map<String, dynamic> json) => TransferTarget(
    deviceId: json['deviceId'] as String,
    route: json['selectedRoute'] as String,
    status: json['status'] as String,
    bytesTransferred: json['bytesTransferred'] as int? ?? 0,
  );

  final String deviceId;
  final String route;
  final String status;
  final int bytesTransferred;
}

class TransferFile {
  const TransferFile({
    required this.id,
    required this.name,
    required this.size,
    required this.chunkCount,
    required this.chunkSize,
    required this.sha256,
  });

  factory TransferFile.fromJson(Map<String, dynamic> json) => TransferFile(
    id: json['id'] as String,
    name: json['name'] as String,
    size: json['size'] as int,
    chunkCount: json['chunkCount'] as int,
    chunkSize: json['chunkSize'] as int,
    sha256: json['sha256'] as String,
  );

  final String id;
  final String name;
  final int size;
  final int chunkCount;
  final int chunkSize;
  final String sha256;
}
