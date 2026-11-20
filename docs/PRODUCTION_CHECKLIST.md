# EQUIPRENT Production Checklist

## Release gates

- [ ] CI passes Go vet/unit tests, sqlc validation, Flutter analysis/widget tests, migration/integration tests, and secret/dependency scans.
- [ ] Test all five roles and tenant isolation end to end with real API/database; document screens still using mocks.
- [ ] Live Razorpay checkout/webhook/replay/idempotency verified; Flutter checkout SDK integrated.
- [ ] Real SMS/email delivery providers integrated and verified; development OTP/token echo disabled.
- [ ] Firebase push tested with a real project; permission and preference behavior verified.
- [ ] Google Maps keys restricted by platform/referrer/package; fallback and billing alerts configured.
- [ ] Support, analytics, and other incomplete modules completed or explicitly excluded from launch.
- [ ] Privacy review completed for location, KYC, evidence, chat, and retention/deletion.
- [ ] Runtime DB role has least privilege; TLS verified; migrations run separately; backup restore drill passed.
- [ ] Shared Redis configured; rate limiting and WebSocket fanout tested under outage and scale.
- [ ] HTTPS, DNS, certificate renewal, CORS, trusted proxies, security headers, and production config validated.
- [ ] Object storage private/encrypted; upload validation, malware policy, signed URL expiry, and lifecycle verified.
- [ ] Monitoring, alerting, structured logs, redaction, retention, on-call, incident response, and rollback operational.
- [ ] Performance/load tests establish SLO headroom; accessibility and Android/iOS/Web matrix pass.
- [ ] Legal/tax terms reviewed by qualified professionals and required reviews recorded.
- [ ] No paid service purchase or production deployment without explicit business approval.

## Sign-off

Owner: ____________________  Date: __________

Engineering: ______________  Security: _______
