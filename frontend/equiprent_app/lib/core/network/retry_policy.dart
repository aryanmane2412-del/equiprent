import 'api_exception.dart';

/// Riverpod retry policy for API-backed providers.
///
/// Riverpod 3 retries every failed provider up to 10 times by default, which
/// keeps screens on "Loading…" for ~30 s when the server has already said
/// no. Only transient failures (network, timeout, rate limit, 5xx) are
/// retried, three times with backoff; 4xx answers surface immediately.
Duration? apiRetry(int retryCount, Object error) {
  if (retryCount >= 3) return null;
  if (error is ApiException) {
    const transient = {ApiErrorKind.network, ApiErrorKind.timeout, ApiErrorKind.rateLimited, ApiErrorKind.server};
    if (!transient.contains(error.kind)) return null;
  } else if (error is Error) {
    return null; // programming errors never fix themselves
  }
  return Duration(milliseconds: 400 * (1 << retryCount));
}
