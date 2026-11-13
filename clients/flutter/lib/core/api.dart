abstract class HubApi {
  Future<dynamic> request(
    String method, {
    Map<String, dynamic> params = const {},
    String target = '',
    Map<String, dynamic> command = const {},
  });
  bool get canHost;
  Future<void> initialize();
}
