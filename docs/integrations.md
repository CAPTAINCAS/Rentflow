# Payment and SMS integration notes

## Safaricom Daraja

The official Daraja Developer Portal describes Daraja 3.0 as the Safaricom/M-PESA API platform and directs developers to create a sandbox application for credentials and testing. Its text-rendered API pages expose only the platform overview, so payload details below are cross-checked against a current integration reference and must be exercised against the sandbox once a public callback URL is available. [1] [2]

Use the sandbox base `https://sandbox.safaricom.co.ke`. Request an access token with `GET /oauth/v1/generate?grant_type=client_credentials` using HTTP Basic authentication, and reuse the token until close to expiry. Start an STK payment with `POST /mpesa/stkpush/v1/processrequest` and bearer authorization. Build the Nairobi-local `YYYYMMDDHHmmss` timestamp, then set `Password` to Base64 of shortcode + passkey + the same timestamp. The Paybill request uses `TransactionType=CustomerPayBillOnline`, `BusinessShortCode`, `PartyA` and `PhoneNumber` in `254...` format, `PartyB`, `Amount`, `CallBackURL`, `AccountReference`, and `TransactionDesc`. The STK request's accepted response only means the prompt was accepted; record its `CheckoutRequestID` and mark the payment only after a successful callback. A successful callback reports `ResultCode=0` and a `CallbackMetadata.Item` collection including `MpesaReceiptNumber`, `Amount`, `TransactionDate`, and `PhoneNumber`. The reference notes an `AccountReference` limit of 12 characters and a `TransactionDesc` limit of 13, so unit-code validation must prevent references that cannot be sent. [2]

Implement the direct Paybill callback routes separately from STK: a C2B validation endpoint, a C2B confirmation endpoint, and an operator-invoked URL registration flow. Store only accepted confirmations, enforce unique M-Pesa transaction IDs/receipts, and map `BillRefNumber` to the unit code; accept unmatched codes into the manual-review queue if they are validly received. Cross-check registration and callback behavior in the Daraja sandbox before production configuration. The future HTTPS callback/tunnel URL is not yet supplied.

## Africa's Talking SMS

The official docs describe SMS POST requests to `/version1/messaging/bulk`, with JSON supported. The request uses an `apiKey` header and fields including `username`, `phoneNumbers`, `message`, and a registered `senderId`; the sandbox username is `sandbox`. The official page currently marks the sandbox endpoint `https://api.sandbox.africastalking.com/version1/messaging/bulk` as “coming soon.” Therefore local development must use the requested mock provider. Keep the Africa's Talking base URL and credentials configurable, do not enable real dispatch without explicit configuration, and surface provider errors/status codes rather than claiming delivery from request acceptance alone. [3] [4]

## References

[1]: https://developer.safaricom.co.ke/ "Safaricom Daraja Developer Portal"
[2]: https://www.codecapsules.io/blog/mpesa-daraja-api-stk-push-nodejs/ "M-Pesa Daraja API: STK Push Guide, November 2026"
[3]: https://developers.africastalking.com/docs/authentication "Africa's Talking API authentication"
[4]: https://developers.africastalking.com/docs/sms/sending/bulk "Africa's Talking Bulk SMS API"
