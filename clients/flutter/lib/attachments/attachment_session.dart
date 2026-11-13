import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:cross_file/cross_file.dart';
import 'package:cryptography/cryptography.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:pasteboard/pasteboard.dart';
import '../core/api.dart';

const maxAttachmentBytes = 50 << 20;
const attachmentChunkBytes = 48 << 10;
bool get supportsDesktopAttachmentInput =>
    defaultTargetPlatform != TargetPlatform.android &&
    defaultTargetPlatform != TargetPlatform.iOS;

class Attachment {
  const Attachment({
    required this.id,
    required this.name,
    required this.kind,
    required this.size,
    this.mime = 'application/octet-stream',
    this.sha256 = '',
    this.state = 'ready',
    this.source = '',
    this.inline = false,
    this.width = 0,
    this.height = 0,
    this.thumbnail = false,
  });
  factory Attachment.fromMap(Map value) => Attachment(
    id: value['id'] as String,
    name: value['name'] as String? ?? '附件',
    kind: value['kind'] as String? ?? 'file',
    size: (value['size'] as num? ?? 0).toInt(),
    mime: value['mime'] as String? ?? 'application/octet-stream',
    sha256: value['sha256'] as String? ?? '',
    state: value['state'] as String? ?? 'missing',
    source: value['source'] as String? ?? '',
    inline: value['inline'] == true,
    width: (value['width'] as num? ?? 0).toInt(),
    height: (value['height'] as num? ?? 0).toInt(),
    thumbnail: value['thumbnail'] == true,
  );
  final String id, name, kind, mime, sha256, state, source;
  final int size, width, height;
  final bool inline, thumbnail;
  bool get image => kind == 'image';
  String get unavailableReason => switch (state) {
    'missing' => '源文件已失效',
    'tooLarge' => '文件超过 50 MB',
    _ => '附件暂不可用',
  };
}

String attachmentSize(int bytes) => bytes >= 1 << 20
    ? '${(bytes / (1 << 20)).toStringAsFixed(1)} MB'
    : bytes >= 1024
    ? '${(bytes / 1024).toStringAsFixed(1)} KB'
    : '$bytes B';
String _hex(List<int> bytes) =>
    bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
Future<String> _hash(List<int> bytes) async =>
    _hex((await Sha256().hash(bytes)).bytes);

class TransferCancelled implements Exception {}

class TransferToken {
  bool cancelled = false;
  void check() {
    if (cancelled) throw TransferCancelled();
  }
}

class UploadTask extends TransferToken {
  UploadTask(this.name, this.size);
  final String name;
  final int size;
  Uint8List? bytes;
  String id = '', error = '', hash = '';
  int sent = 0;
  bool busy = true;
  Attachment? attachment;
  double get progress => size == 0 ? 0 : (sent / size).clamp(0, 1);
}

class _Partial {
  _Partial(this.size, this.hash) : bytes = Uint8List(size);
  final int size;
  final String hash;
  final Uint8List bytes;
  int offset = 0;
}

/// One draft and transfer namespace per device/Provider/session. File bytes
/// travel in bounded RPC chunks over the established encrypted connection.
class AttachmentSession extends ChangeNotifier {
  AttachmentSession({
    required this.api,
    required this.target,
    required this.provider,
    required this.session,
  });
  final HubApi api;
  final String target, provider, session;
  final List<UploadTask> uploads = [];
  final _inflight = <String, Future<Uint8List>>{};
  final _partials = <String, _Partial>{};
  final _queues = <String, Future<void>>{};
  final _downloadTokens = <TransferToken>{};
  bool _disposed = false;
  String error = '';
  DateTime _lastNotify = DateTime.fromMillisecondsSinceEpoch(0);
  bool get blocked => uploads.any((u) => u.busy || u.attachment == null);
  List<String> get readyIDs => uploads
      .where((u) => u.attachment != null)
      .map((u) => u.attachment!.id)
      .toList();
  void _changed({bool force = true}) {
    if (_disposed) return;
    final now = DateTime.now();
    if (force || now.difference(_lastNotify).inMilliseconds >= 100) {
      _lastNotify = now;
      notifyListeners();
    }
  }

  Future<dynamic> _call(String method, Map<String, dynamic> params) =>
      api.request(
        'client.call',
        target: target,
        command: {
          'method': method,
          'provider': provider,
          'session': session,
          'params': params,
        },
      );
  Future<void> choose() async {
    try {
      if (!kIsWeb && defaultTargetPlatform == TargetPlatform.macOS) {
        await FilePicker.skipEntitlementsChecks();
      }
      final files = await FilePicker.pickFiles(dialogTitle: '选择图片或文件');
      for (final file in files) {
        if (_disposed) break;
        await addFile(file.xFile);
      }
    } catch (e) {
      error = '选择附件失败：$e';
      _changed();
    }
  }

  Future<void> addFiles(Iterable<XFile> files) async {
    for (final file in files) {
      if (_disposed) break;
      await addFile(file);
    }
  }

  bool _accept(int size) {
    if (_disposed) return false;
    if (uploads.length >= 16) {
      error = '每条消息最多 16 个附件';
      _changed();
      return false;
    }
    if (size < 0 || size > maxAttachmentBytes) {
      error = '单个附件不能超过 50 MB';
      _changed();
      return false;
    }
    if (uploads.fold<int>(0, (sum, u) => sum + u.size) + size > 100 << 20) {
      error = '一条消息的附件总量不能超过 100 MB';
      _changed();
      return false;
    }
    error = '';
    return true;
  }

  Future<void> addFile(XFile file) async {
    UploadTask? task;
    try {
      final size = await file.length();
      if (!_accept(size)) return;
      task = UploadTask(file.name, size);
      uploads.add(task);
      _changed();
      final builder = BytesBuilder(copy: false);
      var count = 0;
      await for (final bytes in file.openRead()) {
        task.check();
        count += bytes.length;
        if (count > size || count > maxAttachmentBytes) {
          throw StateError('读取时文件大小发生变化');
        }
        builder.add(bytes);
      }
      task.check();
      if (count != size) throw StateError('文件未完整读取');
      task.bytes = builder.takeBytes();
      await retry(task);
    } catch (e) {
      if (task != null && !task.cancelled) {
        task.busy = false;
        task.error = '读取失败：$e';
      }
      if (task == null) error = '无法读取文件：$e';
      _changed();
    }
  }

  Future<void> addBytes(String name, Uint8List bytes) async {
    if (!_accept(bytes.length)) return;
    final task = UploadTask(name, bytes.length)..bytes = bytes;
    uploads.add(task);
    _changed();
    await retry(task);
  }

  Future<void> retry(UploadTask task) async {
    if (_disposed ||
        task.cancelled ||
        task.bytes == null ||
        task.attachment != null) {
      return;
    }
    task.busy = true;
    task.error = '';
    _changed();
    try {
      if (task.hash.isEmpty) task.hash = await _hash(task.bytes!);
      task.check();
      if (task.id.isEmpty) {
        final result =
            await _call('attachment.begin', {
                  'name': task.name,
                  'size': task.size,
                  'sha256': task.hash,
                })
                as Map;
        task.id = result['id'] as String;
      } else {
        final state = await _call('attachment.status', {'id': task.id}) as Map;
        task.sent = (state['offset'] as num).toInt();
        if (state['attachment'] is Map) {
          task.attachment = Attachment.fromMap(state['attachment'] as Map);
        }
      }
      task.check();
      while (task.sent < task.size && task.attachment == null) {
        task.check();
        final end = (task.sent + attachmentChunkBytes).clamp(0, task.size);
        final state =
            await _call('attachment.write', {
                  'id': task.id,
                  'offset': task.sent,
                  'data': base64Encode(
                    Uint8List.sublistView(task.bytes!, task.sent, end),
                  ),
                })
                as Map;
        final offset = (state['offset'] as num).toInt();
        if (offset < end || offset > task.size) throw StateError('上传偏移无效');
        task.sent = offset;
        _changed(force: false);
      }
      task.check();
      final result = await _call('attachment.finish', {'id': task.id}) as Map;
      final attachment = Attachment.fromMap(result['attachment'] as Map);
      if (attachment.sha256 != task.hash || attachment.size != task.size) {
        throw StateError('上传结果校验失败');
      }
      task.attachment = attachment;
      task.sent = task.size;
    } catch (e) {
      if (!task.cancelled) task.error = '上传中断，可重试：$e';
    } finally {
      task.busy = false;
      _changed();
      if (task.cancelled && task.id.isNotEmpty) unawaited(_abort(task.id));
    }
  }

  Future<void> _abort(String id) async {
    try {
      await _call('attachment.abort', {'id': id});
    } catch (_) {}
  }

  void remove(UploadTask task) {
    task.cancelled = true;
    uploads.remove(task);
    if (task.id.isNotEmpty && task.attachment == null) {
      unawaited(_abort(task.id));
    }
    _changed();
  }

  void submitted(List<String> ids) {
    uploads.removeWhere((u) => ids.contains(u.attachment?.id));
    _changed();
  }

  Future<void> paste(TextEditingController input) async {
    if (!kIsWeb && supportsDesktopAttachmentInput) {
      // Prefer actual copied files over Finder/Explorer's image representation.
      try {
        final paths = await Pasteboard.files();
        if (paths.isNotEmpty) {
          await addFiles(paths.map((p) => XFile(p)));
          return;
        }
      } catch (_) {
        /* Some platforms expose images but not file URLs. */
      }
      try {
        final image = await Pasteboard.image;
        if (image != null && image.isNotEmpty) {
          await addBytes(
            'clipboard-${DateTime.now().millisecondsSinceEpoch}.png',
            image,
          );
          return;
        }
      } catch (_) {
        /* Keep ordinary text paste available. */
      }
    }
    final clipboard = await Clipboard.getData(Clipboard.kTextPlain);
    if (_disposed || clipboard?.text == null) return;
    final value = input.value,
        selection = input.selection.isValid
            ? input.selection
            : TextSelection.collapsed(offset: input.text.length);
    final text = clipboard!.text!;
    input.value = TextEditingValue(
      text: value.text.replaceRange(selection.start, selection.end, text),
      selection: TextSelection.collapsed(offset: selection.start + text.length),
    );
  }

  String _cacheKey(Attachment a, bool thumbnail) =>
      '$provider|$session|${a.sha256.isEmpty ? a.id : a.sha256}|${thumbnail ? 'thumbnail' : 'original'}';
  Future<Uint8List> bytes(
    Attachment attachment, {
    bool thumbnail = false,
    TransferToken? token,
    ValueChanged<double>? onProgress,
  }) {
    final preview = thumbnail && attachment.thumbnail;
    final key = _cacheKey(attachment, preview);
    if (token == null && _inflight.containsKey(key)) return _inflight[key]!;
    // Serialize transfers of the same object so independent preview/download
    // cancellation tokens never mutate the same resume buffer concurrently.
    final previous = _queues[key] ?? Future<void>.value();
    final job = previous.then(
      (_) => _download(
        attachment,
        preview,
        key,
        token ?? TransferToken(),
        onProgress,
      ),
    );
    final settled = job.then<void>((_) {}, onError: (Object _) {});
    _queues[key] = settled;
    unawaited(
      settled.then((_) {
        if (identical(_queues[key], settled)) _queues.remove(key);
      }),
    );
    if (token == null) {
      _inflight[key] = job;
      unawaited(
        settled.then((_) {
          if (identical(_inflight[key], job)) _inflight.remove(key);
        }),
      );
    }
    return job;
  }

  Future<Uint8List> _download(
    Attachment a,
    bool preview,
    String key,
    TransferToken token,
    ValueChanged<double>? onProgress,
  ) async {
    _downloadTokens.add(token);
    try {
      token.check();
      if (_disposed) throw TransferCancelled();
      try {
        final cached =
            await api.request(
                  'client.assetCache.get',
                  target: target,
                  params: {'key': key},
                )
                as Map?;
        if (cached?['data'] is String) {
          final bytes = base64Decode(cached!['data'] as String);
          if (preview || await _hash(bytes) == a.sha256) {
            token.check();
            return bytes;
          }
        }
      } catch (_) {
        /* A disabled/full browser cache must not block the transfer. */
      }
      token.check();
      if (a.state != 'ready') throw StateError(a.unavailableReason);
      var partial = _partials[key];
      while (true) {
        token.check();
        if (_disposed) throw TransferCancelled();
        Map result = {};
        for (var attempt = 0; ; attempt++) {
          try {
            result =
                await _call('attachment.read', {
                      'id': a.id,
                      'offset': partial?.offset ?? 0,
                      'thumbnail': preview,
                    })
                    as Map;
            break;
          } catch (_) {
            if (attempt >= 2) rethrow;
            await Future<void>.delayed(
              Duration(milliseconds: 300 * (attempt + 1)),
            );
            token.check();
          }
        }
        token.check();
        final size = (result['size'] as num).toInt(),
            offset = (result['offset'] as num).toInt(),
            hash = result['sha256'] as String;
        if (size < 0 ||
            size > maxAttachmentBytes ||
            offset != (partial?.offset ?? 0)) {
          throw StateError('附件数据或大小无效');
        }
        if (partial == null) {
          if (_partials.length >= 2) {
            final oldest = _partials.keys.first;
            _partials.remove(oldest);
          }
          partial = _Partial(size, hash);
          _partials[key] = partial;
        }
        if (partial.size != size ||
            partial.hash != hash ||
            (!preview && a.sha256 != hash)) {
          _partials.remove(key);
          throw StateError('附件内容发生变化');
        }
        final block = base64Decode(result['data'] as String);
        if (block.length > attachmentChunkBytes ||
            offset + block.length > size ||
            (block.isEmpty && result['eof'] != true) ||
            (offset + block.length == size && result['eof'] != true)) {
          throw StateError('附件分块无效');
        }
        partial.bytes.setRange(offset, offset + block.length, block);
        partial.offset += block.length;
        onProgress?.call(size == 0 ? 1 : partial.offset / size);
        if (result['eof'] == true) {
          if (partial.offset != size || await _hash(partial.bytes) != hash) {
            _partials.remove(key);
            throw StateError('附件校验失败，请重试');
          }
          final bytes = partial.bytes;
          token.check();
          if (_disposed) throw TransferCancelled();
          _partials.remove(key);
          try {
            await api.request(
              'client.assetCache.put',
              target: target,
              params: {'key': key, 'data': base64Encode(bytes)},
            );
          } catch (_) {}
          return bytes;
        }
      }
    } finally {
      _downloadTokens.remove(token);
    }
  }

  Future<Uri?> save(
    Attachment a, {
    TransferToken? token,
    ValueChanged<double>? onProgress,
  }) async {
    final data = await bytes(a, token: token, onProgress: onProgress);
    token?.check();
    if (!kIsWeb && defaultTargetPlatform == TargetPlatform.macOS) {
      await FilePicker.skipEntitlementsChecks();
    }
    return FilePicker.saveFile(
      fileName: a.name,
      bytes: data,
      mimeType: a.mime,
      dialogTitle: '保存附件',
    );
  }

  Future<void> clearCache() async {
    await api.request('client.assetCache.clear', target: target);
    _partials.clear();
  }

  @override
  void dispose() {
    _disposed = true;
    for (final u in uploads) {
      u.cancelled = true;
    }
    for (final token in _downloadTokens) {
      token.cancelled = true;
    }
    _partials.clear();
    super.dispose();
  }
}
