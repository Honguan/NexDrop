package io.github.honguan.nexdrop

import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.OpenableColumns
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel
import java.io.File

class MainActivity : FlutterActivity() {
    private var pendingShare: Map<String, Any>? = null
    private var eventSink: EventChannel.EventSink? = null

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        pendingShare = extractShare(intent)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "com.nexdrop/client").setMethodCallHandler { call, result ->
            when (call.method) {
                "getInitialShare" -> result.success(consumeShare())
                "startTransferService" -> {
                    val service = Intent(this, TransferForegroundService::class.java)
                    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) startForegroundService(service) else startService(service)
                    result.success(null)
                }
                "stopTransferService" -> {
                    stopService(Intent(this, TransferForegroundService::class.java))
                    result.success(null)
                }
                else -> result.notImplemented()
            }
        }
        EventChannel(flutterEngine.dartExecutor.binaryMessenger, "com.nexdrop/client/shares").setStreamHandler(
            object : EventChannel.StreamHandler {
                override fun onListen(arguments: Any?, events: EventChannel.EventSink) {
                    eventSink = events
                    consumeShare()?.let(events::success)
                }

                override fun onCancel(arguments: Any?) {
                    eventSink = null
                }
            },
        )
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        val share = extractShare(intent) ?: return
        if (eventSink != null) {
            eventSink?.success(share)
        } else {
            pendingShare = share
        }
    }

    private fun consumeShare(): Map<String, Any>? {
        val value = pendingShare
        pendingShare = null
        return value
    }

    private fun extractShare(intent: Intent?): Map<String, Any>? {
        if (intent == null) return null
        if (intent.action == Intent.ACTION_VIEW && intent.data?.scheme == "nexdrop" && intent.data?.host == "join") {
            return mapOf("text" to "", "files" to emptyList<String>(), "joinUri" to intent.dataString.orEmpty())
        }
        if (intent.action != Intent.ACTION_SEND && intent.action != Intent.ACTION_SEND_MULTIPLE) return null
        val text = intent.getStringExtra(Intent.EXTRA_TEXT)?.trim().orEmpty()
        val uris = linkedSetOf<Uri>()
        if (intent.action == Intent.ACTION_SEND) {
            streamUri(intent)?.let(uris::add)
        } else {
            streamUris(intent).forEach(uris::add)
        }
        intent.clipData?.let { clip ->
            for (index in 0 until clip.itemCount) clip.getItemAt(index).uri?.let(uris::add)
        }
        val files = uris.mapNotNull(::copyToShareCache)
        if (text.isEmpty() && files.isEmpty()) return null
        return mapOf("text" to text, "files" to files)
    }

    @Suppress("DEPRECATION")
    private fun streamUri(intent: Intent): Uri? =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
        else intent.getParcelableExtra(Intent.EXTRA_STREAM)

    @Suppress("DEPRECATION")
    private fun streamUris(intent: Intent): List<Uri> =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM, Uri::class.java).orEmpty()
        else intent.getParcelableArrayListExtra<Uri>(Intent.EXTRA_STREAM).orEmpty()

    private fun copyToShareCache(uri: Uri): String? = try {
        val directory = File(cacheDir, "shared").apply { mkdirs() }
        val displayName = contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { cursor ->
            if (cursor.moveToFirst()) cursor.getString(0) else null
        }
        val safeName = (displayName ?: "shared-${System.nanoTime()}").replace(Regex("[^A-Za-z0-9._ -]"), "_").take(120)
        val target = File(directory, "${System.nanoTime()}-$safeName")
        contentResolver.openInputStream(uri)?.use { input ->
            target.outputStream().use { output -> input.copyTo(output) }
        } ?: return null
        target.absolutePath
    } catch (_: Exception) {
        null
    }
}
