import 'dart:convert';
import 'dart:ffi';
import 'dart:io';
import 'dart:isolate';
import 'package:ffi/ffi.dart';
import 'package:path_provider/path_provider.dart';
import 'api.dart';

typedef _CallC = Pointer<Utf8> Function(Pointer<Utf8>);
typedef _CallD = Pointer<Utf8> Function(Pointer<Utf8>);
typedef _FreeC = Void Function(Pointer<Utf8>);
typedef _FreeD = void Function(Pointer<Utf8>);
String _invoke((String, String) args) {
  final lib = args.$1 == 'process'
      ? DynamicLibrary.process()
      : DynamicLibrary.open(args.$1);
  final call = lib.lookupFunction<_CallC, _CallD>('AgentHubCall');
  final free = lib.lookupFunction<_FreeC, _FreeD>('AgentHubFree');
  final input = args.$2.toNativeUtf8();
  try {
    final result = call(input);
    try {
      return result.toDartString();
    } finally {
      free(result);
    }
  } finally {
    malloc.free(input);
  }
}

HubApi createApi() => NativeApi();

class NativeApi implements HubApi {
  String _dir = '', _lib = '', _uiPath = '';
  @override
  bool get canHost =>
      Platform.isMacOS || Platform.isWindows || Platform.isLinux;
  @override
  Future<void> initialize() async {
    const customDir = String.fromEnvironment('AGENTHUB_DATA_DIR');
    final support = await getApplicationSupportDirectory();
    _dir = customDir.isEmpty ? '${support.path}/agenthub' : customDir;
    const customLib = String.fromEnvironment('AGENTHUB_GO_LIB');
    if (customLib.isNotEmpty) {
      _lib = customLib;
    } else if (Platform.isIOS) {
      _lib = 'process';
    } else if (Platform.isAndroid) {
      _lib = 'libagenthub.so';
    } else if (Platform.isMacOS) {
      _lib =
          '${File(Platform.resolvedExecutable).parent.parent.path}/Frameworks/libagenthub.dylib';
    } else {
      _lib =
          '${File(Platform.resolvedExecutable).parent.path}/${Platform.isWindows ? 'agenthub.dll' : 'libagenthub.so'}';
    }
    if (Platform.isMacOS) {
      _uiPath = File(Platform.resolvedExecutable).parent.parent.parent.path;
    }
  }

  @override
  Future<dynamic> request(
    String method, {
    Map<String, dynamic> params = const {},
    String target = '',
    Map<String, dynamic> command = const {},
  }) async {
    final raw = jsonEncode({
      'method': method,
      'dataDir': _dir,
      'hostDir': _dir,
      'uiPath': _uiPath,
      'target': target,
      'params': params,
      'request': command,
    });
    final result =
        jsonDecode(await Isolate.run(() => _invoke((_lib, raw)))) as Map;
    if (result['error'] != null) {
      throw StateError(result['error'] as String);
    }
    return result['result'];
  }
}
