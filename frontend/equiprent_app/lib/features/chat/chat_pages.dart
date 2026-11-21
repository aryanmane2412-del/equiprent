import '../../core/utils/app_lifecycle.dart';
import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/session/active_role_provider.dart';
import '../../core/session/user_role.dart';
import '../comms/comms_routes.dart';
import '../comms/data/comms_api.dart';
import '../comms/data/comms_models.dart';
import '../comms/presentation/comms_widgets.dart';
import '../comms/presentation/ui.dart';
import '../verification/data/verification_api.dart' show documentPickerProvider;
import '../verification/data/verification_models.dart' show PickedFile;

// ---- conversations ------------------------------------------------------------------------

/// Booking chats and listing enquiries, most recent first.
class ConversationsPage extends ConsumerWidget {
  const ConversationsPage({super.key, this.topLevel = true});
  final bool topLevel;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final list = ref.watch(conversationsProvider);
    final role = ref.watch(activeRoleProvider) ?? UserRole.customer;
    final unread = list.value?.fold<int>(0, (s, c) => s + c.unread) ?? 0;
    Future<void> refresh() async {
      ref.invalidate(conversationsProvider);
      ref.invalidate(unreadMessagesProvider);
    }

    return AppPage(
      title: 'Messages',
      subtitle: list.hasValue
          ? (unread == 0 ? 'No unread messages' : '$unread unread')
          : null,
      topLevel: topLevel,
      body: AsyncBody(
        value: list,
        onRetry: refresh,
        builder: (items) => items.isEmpty
            ? const EmptyState(
                icon: AppIcons.chat,
                title: 'No conversations yet',
                message:
                    'Open a booking to chat with the other party, or message an owner from a listing.',
              )
            : RefreshIndicator(
                onRefresh: refresh,
                child: ListView.separated(
                  padding: const EdgeInsets.symmetric(vertical: AppSpacing.sm),
                  itemCount: items.length,
                  separatorBuilder: (_, _) =>
                      const Divider(height: 1, indent: 72),
                  itemBuilder: (context, i) => _ConversationTile(
                    c: items[i],
                    onTap: () =>
                        context.push(CommsRoutes.thread(role, items[i].id)),
                  ),
                ),
              ),
      ),
    );
  }
}

class _ConversationTile extends StatelessWidget {
  const _ConversationTile({required this.c, required this.onTap});
  final Conversation c;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final bold = c.unread > 0;
    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.md,
          vertical: AppSpacing.ms,
        ),
        child: Row(
          children: [
            IconTile(
              icon: c.isEnquiry ? AppIcons.search : AppIcons.chat,
              tone: c.isEnquiry ? AppTone.info : AppTone.brand,
            ),
            const Gap(AppSpacing.ms),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          c.title,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.titleSmall?.copyWith(
                            fontWeight: bold
                                ? FontWeight.w700
                                : FontWeight.w500,
                          ),
                        ),
                      ),
                      Text(
                        Fmt.ago(c.updatedAt, DateTime.now()),
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: AppColors.slate500,
                        ),
                      ),
                    ],
                  ),
                  const Gap(2),
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          c.lastMessage.isEmpty
                              ? 'No messages yet'
                              : c.lastMessage,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: AppColors.slate600,
                          ),
                        ),
                      ),
                      if (c.unread > 0)
                        Container(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 7,
                            vertical: 2,
                          ),
                          decoration: const BoxDecoration(
                            color: AppColors.orange500,
                            borderRadius: BorderRadius.all(Radius.circular(10)),
                          ),
                          child: Text(
                            '${c.unread}',
                            style: const TextStyle(
                              color: AppColors.white,
                              fontSize: 11,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

// ---- thread -------------------------------------------------------------------------------

class ChatThreadPage extends ConsumerStatefulWidget {
  const ChatThreadPage({super.key, required this.conversationId});
  final String conversationId;
  @override
  ConsumerState<ChatThreadPage> createState() => _ChatThreadPageState();
}

class _ChatThreadPageState extends ConsumerState<ChatThreadPage> {
  final _text = TextEditingController();
  final _messages = <ChatMessage>[]; // oldest first
  Timer? _poll;
  bool _loading = true;
  bool _loadingOlder = false;
  bool _hasOlder = true;
  Object? _error;

  CommsApi get _api => ref.read(commsApiProvider);

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _poll?.cancel();
    _text.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final page = await _api.messages(widget.conversationId);
      _messages
        ..clear()
        ..addAll(page.reversed);
      _hasOlder = page.length >= 40;
      await _markRead();
      final every = ref.read(chatPollProvider);
      if (every != null) _poll = Timer.periodic(every, (_) => _pollNew());
    } catch (e) {
      _error = e;
    }
    if (mounted) setState(() => _loading = false);
  }

  String? get _lastServerId =>
      _messages.where((m) => !m.sending && !m.failed).lastOrNull?.id;

  Future<void> _pollNew() async {
    if (appInBackground) return;
    final after = _lastServerId;
    try {
      final fresh = after == null
          ? (await _api.messages(widget.conversationId)).reversed.toList()
          : await _api.messages(widget.conversationId, after: after);
      final known = {for (final m in _messages) m.id};
      final added = fresh.where((m) => !known.contains(m.id)).toList();
      if (added.isEmpty || !mounted) return;
      setState(() => _messages.addAll(added));
      await _markRead();
    } catch (_) {
      // Network blips: the next poll retries.
    }
  }

  Future<void> _loadOlder() async {
    final first = _messages.where((m) => !m.sending).firstOrNull;
    if (first == null) return;
    setState(() => _loadingOlder = true);
    try {
      final older = await _api.messages(
        widget.conversationId,
        before: first.id,
      );
      setState(() {
        _messages.insertAll(0, older.reversed);
        _hasOlder = older.length >= 40;
      });
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
    } finally {
      if (mounted) setState(() => _loadingOlder = false);
    }
  }

  Future<void> _markRead() async {
    try {
      await _api.markConversationRead(widget.conversationId);
      ref.invalidate(unreadMessagesProvider);
      ref.invalidate(conversationsProvider);
    } catch (_) {
      // Best effort: the unread badge simply refreshes on the next poll.
    }
  }

  /// Sends optimistically; the client id makes retries safe.
  Future<void> _send({
    String? body,
    List<int>? image,
    String? imageName,
    ChatMessage? retry,
  }) async {
    final clientId = retry?.clientId ?? newClientId();
    final pending = ChatMessage(
      id: 'local-$clientId',
      conversationId: widget.conversationId,
      mine: true,
      createdAt: DateTime.now(),
      body: retry?.body ?? body,
      clientId: clientId,
      sending: true,
      localImage: retry?.localImage ?? image,
    );
    setState(() {
      if (retry != null) _messages.removeWhere((m) => m.clientId == clientId);
      _messages.add(pending);
    });
    try {
      String? uploadId;
      final img = pending.localImage;
      if (img != null) {
        uploadId = await _api.upload(
          'chat_image',
          PickedFile(name: imageName ?? 'photo.jpg', bytes: img),
        );
      }
      final sent = await _api.sendMessage(
        widget.conversationId,
        body: pending.body,
        uploadId: uploadId,
        clientId: clientId,
      );
      if (!mounted) return;
      setState(() {
        final i = _messages.indexWhere((m) => m.clientId == clientId);
        if (i >= 0) _messages[i] = sent;
        if (_messages.where((m) => m.id == sent.id).length > 1) {
          _messages.removeAt(_messages.lastIndexWhere((m) => m.id == sent.id));
        }
      });
      ref.invalidate(conversationsProvider);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        final i = _messages.indexWhere((m) => m.clientId == clientId);
        if (i >= 0) {
          _messages[i] = ChatMessage(
            id: pending.id,
            conversationId: pending.conversationId,
            mine: true,
            createdAt: pending.createdAt,
            body: pending.body,
            clientId: clientId,
            failed: true,
            localImage: pending.localImage,
          );
        }
      });
      AppToast.error(context, describeError(e));
    }
  }

  Future<void> _sendText() async {
    final text = _text.text.trim();
    if (text.isEmpty) return;
    _text.clear();
    await _send(body: text);
  }

  Future<void> _attach() async {
    final file = await ref.read(documentPickerProvider).pick(imagesOnly: true);
    if (file == null) return;
    final caption = _text.text.trim();
    _text.clear();
    await _send(
      body: caption.isEmpty ? null : caption,
      image: file.bytes,
      imageName: file.name,
    );
  }

  Future<void> _delete(ChatMessage m) async {
    final ok = await showConfirmDialog(
      context,
      title: 'Remove message?',
      message: 'It will show as removed for everyone.',
      confirmLabel: 'Remove',
      tone: AppTone.danger,
    );
    if (!ok || !mounted) return;
    final done = await runComms(
      context,
      () => _api.deleteMessage(widget.conversationId, m.id),
    );
    if (done && mounted) {
      setState(() {
        final i = _messages.indexWhere((x) => x.id == m.id);
        if (i >= 0) {
          _messages[i] = ChatMessage(
            id: m.id,
            conversationId: m.conversationId,
            mine: true,
            createdAt: m.createdAt,
            deleted: true,
          );
        }
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final conv = ref.watch(conversationProvider(widget.conversationId));
    final role = ref.watch(activeRoleProvider) ?? UserRole.customer;
    final c = conv.value;
    final bookingRoute = c?.bookingId == null
        ? null
        : CommsRoutes.booking(role, c!.bookingId!);
    return AppPage(
      title: c?.title ?? 'Chat',
      subtitle: c?.bookingStatus == null
          ? (c?.isEnquiry == true ? 'Listing enquiry' : null)
          : bookingStatusLabel(c!.bookingStatus!),
      topLevel: false,
      actions: [
        if (bookingRoute != null)
          AppIconButton(
            key: const Key('chat-open-booking'),
            icon: AppIcons.bookings,
            tooltip: 'Open booking',
            onPressed: () => context.push(bookingRoute),
          ),
      ],
      body: Column(
        children: [
          Expanded(child: _body(isSupport: c?.kind == 'support')),
          _Composer(controller: _text, onSend: _sendText, onAttach: _attach),
        ],
      ),
    );
  }

  Widget _body({bool isSupport = false}) {
    if (_loading) return const LoadingView(message: 'Loading messages…');
    if (_error != null) {
      return ErrorState(message: describeError(_error!), onRetry: _load);
    }
    if (_messages.isEmpty) {
      return EmptyState(
        icon: AppIcons.chat,
        title: isSupport ? 'Contact customer care' : 'Say hello',
        message: isSupport
            ? 'Send a message to the EQUIPRENT support team. A support agent will reply here.'
            : 'Messages are visible to both parties of the booking.',
        compact: true,
      );
    }
    final items = _messages.reversed
        .toList(); // newest at the bottom of a reversed list
    return ListView.builder(
      key: const Key('chat-messages'),
      reverse: true,
      padding: const EdgeInsets.all(AppSpacing.md),
      itemCount: items.length + (_hasOlder ? 1 : 0),
      itemBuilder: (context, i) {
        if (i == items.length) {
          return Center(
            child: TextButton(
              onPressed: _loadingOlder ? null : _loadOlder,
              child: Text(_loadingOlder ? 'Loading…' : 'Load earlier messages'),
            ),
          );
        }
        final m = items[i];
        return _Bubble(
          m: m,
          onRetry: m.failed ? () => _send(retry: m) : null,
          onDelete: m.canDelete ? () => _delete(m) : null,
        );
      },
    );
  }
}

class _Bubble extends StatelessWidget {
  const _Bubble({required this.m, this.onRetry, this.onDelete});
  final ChatMessage m;
  final VoidCallback? onRetry;
  final VoidCallback? onDelete;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final mine = m.mine;
    final bg = mine ? AppColors.navy700 : AppColors.slate100;
    final fg = mine ? AppColors.white : AppColors.slate900;
    final status = m.failed
        ? 'Not sent · tap to retry'
        : (m.sending ? 'Sending…' : Fmt.time(m.createdAt));
    return Align(
      alignment: mine ? Alignment.centerRight : Alignment.centerLeft,
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxWidth: MediaQuery.sizeOf(context).width * 0.78 > 520
              ? 520
              : MediaQuery.sizeOf(context).width * 0.78,
        ),
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 3),
          child: GestureDetector(
            onTap: onRetry,
            onLongPress: onDelete,
            child: Column(
              crossAxisAlignment: mine
                  ? CrossAxisAlignment.end
                  : CrossAxisAlignment.start,
              children: [
                if (!mine && m.senderName != null)
                  Padding(
                    padding: const EdgeInsets.only(left: 4, bottom: 2),
                    child: Text(
                      m.senderName!,
                      style: theme.textTheme.labelSmall?.copyWith(
                        color: AppColors.slate500,
                      ),
                    ),
                  ),
                DecoratedBox(
                  decoration: BoxDecoration(
                    color: m.deleted ? Colors.transparent : bg,
                    border: m.deleted
                        ? Border.all(color: AppColors.slate300)
                        : null,
                    borderRadius: BorderRadius.only(
                      topLeft: const Radius.circular(14),
                      topRight: const Radius.circular(14),
                      bottomLeft: Radius.circular(mine ? 14 : 4),
                      bottomRight: Radius.circular(mine ? 4 : 14),
                    ),
                  ),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 8,
                    ),
                    child: m.deleted
                        ? Text(
                            'Message removed',
                            style: theme.textTheme.bodyMedium?.copyWith(
                              color: AppColors.slate500,
                              fontStyle: FontStyle.italic,
                            ),
                          )
                        : Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              if (m.localImage != null ||
                                  m.attachment != null) ...[
                                ClipRRect(
                                  borderRadius: BorderRadius.circular(8),
                                  child: ConstrainedBox(
                                    constraints: const BoxConstraints(
                                      maxHeight: 240,
                                      maxWidth: 320,
                                    ),
                                    child: m.localImage != null
                                        ? Image.memory(
                                            Uint8List.fromList(m.localImage!),
                                            fit: BoxFit.cover,
                                            errorBuilder: (_, _, _) =>
                                                const _ImageFallback(),
                                          )
                                        : Image.network(
                                            m.attachment!.url,
                                            fit: BoxFit.cover,
                                            semanticLabel: 'Photo',
                                            errorBuilder: (_, _, _) =>
                                                const _ImageFallback(),
                                          ),
                                  ),
                                ),
                                if (m.body != null) const Gap(6),
                              ],
                              if (m.body != null)
                                Text(
                                  m.body!,
                                  style: theme.textTheme.bodyMedium?.copyWith(
                                    color: fg,
                                  ),
                                ),
                            ],
                          ),
                  ),
                ),
                Padding(
                  padding: const EdgeInsets.only(top: 2, left: 4, right: 4),
                  child: Text(
                    status,
                    style: theme.textTheme.labelSmall?.copyWith(
                      color: m.failed ? AppColors.red600 : AppColors.slate400,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _ImageFallback extends StatelessWidget {
  const _ImageFallback();
  @override
  Widget build(BuildContext context) => const SizedBox(
    width: 160,
    height: 100,
    child: ColoredBox(
      color: AppColors.slate200,
      child: Center(child: Icon(AppIcons.image, color: AppColors.slate500)),
    ),
  );
}

class _Composer extends StatelessWidget {
  const _Composer({
    required this.controller,
    required this.onSend,
    required this.onAttach,
  });
  final TextEditingController controller;
  final VoidCallback onSend;
  final VoidCallback onAttach;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: const BoxDecoration(
        color: AppColors.white,
        border: Border(top: BorderSide(color: AppColors.border)),
      ),
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(
            AppSpacing.sm,
            AppSpacing.sm,
            AppSpacing.sm,
            AppSpacing.sm,
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              AppIconButton(
                key: const Key('chat-attach'),
                icon: AppIcons.image,
                tooltip: 'Send a photo',
                onPressed: onAttach,
              ),
              const Gap(AppSpacing.xs),
              Expanded(
                child: TextField(
                  key: const Key('chat-input'),
                  controller: controller,
                  minLines: 1,
                  maxLines: 5,
                  maxLength: 4000,
                  textInputAction: TextInputAction.send,
                  onSubmitted: (_) => onSend(),
                  decoration: const InputDecoration(
                    hintText: 'Message',
                    counterText: '',
                    isDense: true,
                  ),
                ),
              ),
              const Gap(AppSpacing.xs),
              AppIconButton(
                key: const Key('chat-send'),
                icon: AppIcons.send,
                tooltip: 'Send',
                filled: true,
                onPressed: onSend,
              ),
            ],
          ),
        ),
      ),
    );
  }
}
