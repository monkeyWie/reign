import 'package:agenthub_client/chat_activity.dart';
import 'package:flutter_test/flutter_test.dart';

Map<String, dynamic> workingTurn({
  String id = 'current',
  String status = 'inProgress',
  String commandStatus = 'inProgress',
}) => {
  'id': id,
  'status': status,
  'items': [
    {'id': 'user', 'role': 'user', 'text': 'Review the project'},
    {'id': 'reason', 'role': 'reasoning', 'text': 'Hidden private reasoning'},
    {
      'id': 'commentary',
      'role': 'assistant',
      'phase': 'commentary',
      'text': 'Visible progress',
    },
    {
      'id': 'read',
      'type': 'commandExecution',
      'role': 'tool',
      'status': 'completed',
      'durationMs': 1200,
      'text': 'Hidden raw output',
      'activities': [
        {
          'kind': 'read',
          'title': '读取 main.dart',
          'detail': '/project/main.dart\ncat main.dart',
        },
      ],
    },
    {
      'id': 'run',
      'type': 'commandExecution',
      'role': 'tool',
      'status': commandStatus,
      'text': 'Hidden command output',
      'activities': [
        {'kind': 'command', 'title': '运行 go test', 'detail': 'go test'},
      ],
    },
    {
      'id': 'output',
      'type': 'functionCallOutput',
      'role': 'tool',
      'text': 'Hidden duplicate output',
    },
  ],
};

void main() {
  test(
    'active status uses explicit display summary and ignores reasoning text',
    () {
      Map<String, dynamic> data(String title, {String status = 'inProgress'}) =>
          {
            'turns': [
              {
                'id': 'current',
                'status': status,
                'items': [
                  {
                    'id': 'r',
                    'role': 'reasoning',
                    'text': 'Hidden generic content',
                    'reasoningSummary': title,
                  },
                ],
              },
            ],
          };
      expect(chatProgress(data(''))!.reasoningSummary, isEmpty);
      final first = chatProgress(data('**梳理授权与 TLS 边界**\n\nSummary body'))!;
      expect(first.reasoningSummary, '梳理授权与 TLS 边界');
      final next = chatProgress(data('## 核对端口与扩容'))!;
      expect(next.reasoningSummary, '核对端口与扩容');
      expect(first.revision, isNot(next.revision));
      expect(chatProgress(data('done', status: 'completed')), isNull);
    },
  );
  test('superseded inProgress turns do not keep the conversation running', () {
    for (final status in ['completed', 'failed', 'interrupted']) {
      final data = {
        'turns': [
          workingTurn(id: 'stale', commandStatus: 'failed'),
          workingTurn(id: 'latest', status: status),
        ],
      };
      expect(activeChatTurn(data), isNull);
      expect(chatProgress(data), isNull);
    }
    final data = {
      'turns': [workingTurn(id: 'stale'), workingTurn(id: 'latest')],
    };
    expect(activeChatTurn(data)!['id'], 'latest');
    expect(chatProgress(data)!.turnId, 'latest');
  });

  test('only current turn execution metadata becomes transient activity', () {
    final data = {
      'turns': [workingTurn(id: 'old', status: 'completed'), workingTurn()],
    };
    final progress = chatProgress(data)!;
    expect(progress.turnId, 'current');
    expect(progress.activities, hasLength(2));
    expect(progress.running!.label, '正在运行 go test');
    expect(progress.activities.first.label, '已读取 main.dart');
    expect(progress.activities.first.durationMs, 1200);
    expect(progress.summary, '读取文件 1 · 运行命令 1');
    expect(progress.revision, isNot(contains('Hidden')));
    expect(
      chatProgress({
        'turns': [workingTurn(status: 'completed')],
      }),
      isNull,
    );
    expect(
      chatProgress({
        'turns': [workingTurn(status: 'failed')],
      }),
      isNull,
    );
    expect(
      chatProgress({
        'turns': [workingTurn(status: 'interrupted')],
      }),
      isNull,
    );
  });
  test('legacy items never expose raw output as activity details', () {
    final progress = chatProgress({
      'turns': [
        {
          'id': 't',
          'status': 'inProgress',
          'items': [
            {
              'id': 'legacy',
              'role': 'tool',
              'type': 'customTool',
              'status': 'inProgress',
              'text': 'private raw JSON',
            },
            {
              'id': 'long',
              'role': 'tool',
              'activities': [
                {'kind': 'edit', 'title': '编辑文件', 'detail': 'x' * 20000},
              ],
            },
          ],
        },
      ],
    })!;
    expect(progress.activities.first.detail, isEmpty);
    expect(progress.activities.first.running, false);
    expect(progress.activities.last.detail.length, lessThan(8100));
  });
}
