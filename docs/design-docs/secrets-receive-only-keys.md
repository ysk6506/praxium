---
title: 받는 웹훅의 수신 전용 키 — 확인 포트, 웹훅 키, 60초 캐시
kind: guide
status: active
follows:
  - docs/design-docs/adr/0107-receiving-webhooks-entrance-receive-only-keys-and-limits.md
  - docs/design-docs/secrets-and-encryption.md
---
# 받는 웹훅의 수신 전용 키

[시크릿과 암호화](secrets-and-encryption.md) 6절의 본문이에요. 종류 `hmac_key`(키 필드 `key`)와 `webhook_token`(`hash`)는 수신 전용이에요. 다른 제품이 소스의 변화를 알리려고 부르는 **받는 웹훅**(`POST /v1/sources/{source_id}/webhooks/{webhook_id}:receive`)은 사용자가 아니라 제품이 부르고, 그 증명(서명이나 토큰)을 API 역할이 확인해야 해요. 그래서 위 표의 "API는 복호화하지 않아요"를 이 범위만큼 고쳐요(ADR-0018의 "API에 `open`이 없다", "캐시 없음", "사용마다 한 줄"이 수신 전용 키에만 달라져요).

| 항목 | 규칙 |
|---|---|
| 종류 | `hmac_key`(서명 키 그대로), `webhook_token`(토큰의 SHA-256 해시만). 둘 다 **수신 전용**이에요 |
| 만드는 곳 | 서버만 만들어요. 사람이 값을 고르지 않아서(Basic의 비밀번호도 서버가 만든 값이에요) 흔한 비밀번호를 맞히는 공격이 설 자리가 없어요. 값은 만들 때 한 번만 보여 줘요. 이번 변경은 명령줄(`praxium secret webhook-issue`, `webhook-rotate`, `webhook-retire`, `webhook-delete`)만 있고, REST(`:issue-secret`, `:rotate-secret`)는 소스 REST가 선 뒤의 변경이에요 |
| 시크릿 id | 웹훅마다 하나이고 (소스, 웹훅)에서 정해져요. 정의에는 시크릿도 시크릿 참조도 없어요. 두 웹훅이 한 키를 쓰는 경로이 없어요. `Scope`는 그 소스 하나예요 |
| 감싸는 키 | **웹훅 키**(`purpose: webhook_verify`, 키 id가 `wk-`로 시작하고 파일 형식 이름이 달라요). 소스 접속 시크릿의 마스터 키와 다른 파일이고 서로를 대신하지 못해요. API 프로세스는 웹훅 키만 가지면 되고, 그러면 접속 시크릿은 여전히 풀 수 없어요. 웹훅 키가 새면 웹훅의 서명 키가 새요(받은 함에 직접 쓸 수 있어요). 소스에 접속하는 자격은 새지 않아요 |
| 열기 | 수신 전용 종류는 `Open`으로 열리지 않아요(`secret_receive_only`). 접속용 종류는 확인 포트(`Verifier`)에 쓰이지 않아요. 시크릿의 `Scope`에 그 소스가 없으면 확인은 거절이에요. 세 가지 모두 테스트로 묶어요 |
| 확인 포트 | `Verifier`는 키도 해시도 내주지 않고 "맞다/아니다"만 답해요. 없는 시크릿, 다른 소스, 폐기된 버전, 틀린 값, 종류가 틀린 시크릿 모두 같은 "아니다"예요. 저장소나 키의 오류만 오류로 돌려줘요 |
| 토큰 비교 | 받은 값의 SHA-256과 저장된 해시를 프로세스마다 무작위인 키로 각각 HMAC한 뒤 상수 시간으로 견줘요. 길이가 드러나지 않아요 |
| 캐시 | 풀린 키를 메모리에 두는 시간은 **60초 이하**예요. 요청마다 시크릿 기록의 판(바인딩 태그)을 견주는 값싼 확인을 해서, 고쳐지거나 지워진 기록은 캐시가 남아 있어도 쓰지 않아요. 약속은 "폐기 뒤 길어야 60초"예요(저장소를 여럿이 쓰는 배포에서 그래요. 파일 저장소는 한 프로세스가 데이터 폴더를 쥐어서 명령줄이 서버가 도는 동안 값을 바꾸지 못해요) |
| 기록 | 캐시를 채울 때마다 감사 한 줄(`secret_opened`, 시크릿 id만)을 남기고 요청마다는 세는 값(`webhook_verify_accepted`, `webhook_verify_refused`)만 올려요 |
| 교체 | `webhook-rotate`는 새 버전을 더하고 폐기하지 않은 모든 버전을 확인해서 두 버전이 겹치는 기간을 둬요. 옛 버전은 `webhook-retire`로 폐기해요(폐기한 버전은 곧바로 아무것도 증명하지 못해요) |

**서명의 꼴.** `standard_webhooks` 프로필은 `webhook-id`, `webhook-timestamp`, `webhook-signature`(`v1,<base64>` 여러 개를 공백으로)를 받고 서명 대상은 `id.timestamp.본문`, 알고리즘은 HMAC-SHA256뿐이에요. 시각이 지금에서 300초 넘게 벗어나면 거절하고, 헤더 길이와 서명 개수(8개)에 상한이 있어요. `body_only`는 본문만 서명해서 시각이 없고 재생 창이 없어요(약함으로 표시하고 `signal`에만 허용해요). 재생을 막는 이벤트 id는 서명 안의 것(`webhook-id`나 본문의 경로)만 쓸 수 있어요. 서명이 없는 형태(`header_token`, `basic`, `url_token`)는 재생과 위조를 막지 못하고 그 위험을 받아들여요(위조와 재생은 HMAC만 막아요).


## 알려진 한계
- 웹훅 키가 새면 웹훅의 서명 키가 새요. 캐시 60초와 겹치는 버전 때문에 교체와 폐기가 곧바로 닿지 않는 틈이 최대 60초예요.
- 주소 토큰(`url_token`)은 질의에 있어서 제품 밖의 프록시와 보낸 쪽의 설정에 남아요(제품이 막을 수 없는 남은 위험이고 `signal`에만 허용해요).
