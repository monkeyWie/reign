import 'package:flutter_test/flutter_test.dart';
import 'package:agenthub_client/session_directory.dart';

Map<String, dynamic> session(String id, String? cwd, int? updatedAt) => {
  'id': id,
  'cwd': cwd,
  'updatedAt': updatedAt,
  'providerId': 'codex-cli',
};

void main() {
  test(
    'mixed agents share a directory, duplicate Codex history appears once',
    () {
      final result = aggregateSessions([
        {...session('same', '/aaa', 10), 'sourceKind': 'app'},
        {
          ...session('same', '/aaa', 10),
          'sourceKind': 'app',
          'providerId': 'codex-app',
        },
        {...session('cli', '/aaa', 20), 'sourceKind': 'cli'},
        {...session('claude', '/aaa', 30), 'providerId': 'claude-code'},
        {...session('vscode', '/aaa', 40), 'sourceKind': 'vscode'},
      ]);
      expect(result.map((s) => s['id']), ['claude', 'cli', 'same']);
      expect(result.last['providerId'], 'codex-app');
      expect(groupSessionDirectories(result), hasLength(1));
    },
  );
  test(
    'exact remote directories and recency order survive arbitrary sync order',
    () {
      final input = [
        session('older', '/work/project', 10),
        session('same-name', '/other/project', 20),
        session('no-cwd', null, null),
        session('newest', '/work/project', 40),
        session('windows', r'C:\work\project', 30),
        session('same-time-b', '/work/project', 10),
        session('empty-cwd', '', 5),
        session('case-sensitive', '/Work/project', 15),
      ];
      final groups = groupSessionDirectories(input);
      expect(groups.map((g) => g.path), [
        '/work/project',
        r'C:\work\project',
        '/other/project',
        '/Work/project',
        '',
      ]);
      expect(groups.first.sessions.map((s) => s['id']), [
        'newest',
        'older',
        'same-time-b',
      ]);
      expect(groups.last.sessions.map((s) => s['id']), ['empty-cwd', 'no-cwd']);
      expect(groups[1].title, 'project');
      expect(groups.last.title, '无工作目录');
      expect(
        input.first['id'],
        'older',
      ); // View sorting does not mutate cache order.
      final reversed = groupSessionDirectories(input.reversed);
      expect(reversed.map((g) => g.path), groups.map((g) => g.path));

      input.first['updatedAt'] = 50;
      expect(
        groupSessionDirectories(input).first.sessions.first['id'],
        'older',
      );
    },
  );

  test(
    'cached source labels never infer creation client from provider route',
    () {
      expect(
        sessionSourceLabel({'providerId': 'codex-cli', 'sourceKind': 'app'}),
        'Codex App',
      );
      expect(
        sessionSourceLabel({'providerId': 'codex-app', 'source': '"cli"'}),
        'CLI',
      );
      expect(sessionSourceLabel({'source': '"vscode"'}), 'IDE / App（未细分）');
      expect(
        sessionSourceKind({'source': '{"subAgent":"review"}'}),
        'subAgent',
      );
      expect(sessionSourceKind({'source': '{"custom":"other"}'}), 'custom');
      expect(sessionSourceKind({'providerId': 'codex-cli'}), 'unknown');
      expect(sessionSourceKind({'source': 'futureSource'}), 'unknown');
    },
  );
}
