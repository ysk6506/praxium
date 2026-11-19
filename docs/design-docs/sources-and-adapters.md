---
title: 소스와 어댑터 — DB와 API 소스의 정의, 어댑터의 포트, 읽기 전용, 바깥 요청의 묶기
kind: guide
status: active
follows:
  - docs/design-docs/adr/0058-sources-from-db-and-api-worker-ingestion-and-editing-screens.md
  - docs/design-docs/adr/0008-ingestion.md
---
# 소스와 어댑터

[ADR-0058](adr/0058-sources-from-db-and-api-worker-ingestion-and-editing-screens.md)의 자세한 내용 가운데 소스 쪽이에요. **이 문서는 대부분 아직 구현하지 않은 설계(예정)예요.** 지금 코드는 파일 소스만 읽어요. 8절의 바깥 연결의 방어는 `adapter.egress`가 구현했어요([ADR-0091](adr/0091-egress-defence-in-one-unit-adapter-egress.md)). 수집 서비스는 [수집 서비스와 실행](ingestion-service.md)에, 정의를 쓰는 경로와 시크릿과 경로와 화면은 [정의의 쓰기, 시크릿, 경로, 화면](source-editing.md)에 있어요. 정의 오류의 구조화는 [ADR-0070](adr/0070-definition-errors-with-place-and-code-many-at-once.md)으로 병합됐고, id의 문법과 스키마 버전은 [ADR-0075](adr/0075-definition-id-grammar-and-schema-version.md)로 구현했어요. 시크릿 저장소, 화면의 쓰기 기반, 쓰지 않고 차이를 내는 길은 별도의 변경이 구현하고 있어요. 예시의 이름, 주소, 숫자는 모두 지어낸 것이고 주소는 문서용으로 예약된 `example.test`예요.

## 1. 목적과 범위

로컬 PC에서 PostgreSQL과 HTTP REST API(JSON)와 파일을 소스로 붙이는 데 필요한 소스 정의, 어댑터의 포트, 읽기 전용의 강제, 바깥으로 나가는 요청의 묶기를 정해요.

- 정하는 것: 소스 정의의 모양(2절), 변수와 경로의 문법(3절), 이름만 정해 둔 필드와 지원 기능(4절, 5절), 어댑터의 포트와 레코드의 전달 형식(6절), 읽기 전용(7절), 바깥 요청의 묶기(8절), 한도(9절), HTTP의 선언만으로 되는 것과 안 되는 것(10절), 샘플 소스(11절).
- 정하지 않는 것(자리와 이름만 둬요): 스트림과 증분 수집, 키로 단건 읽기, 스케줄, 파일 올리기, PostgreSQL이 아닌 DB, 원격 수집기, 매핑 제안, 배열 펼치기와 조건 고르기, 앞 요청에 기대는 요청, 값의 시각.

소스 정의는 지금의 두 JSON 문서(소스와 매핑)를 넓힌 것이에요. [수집 설계](ingestion.md) 3.1의 필드 스케치와 달리 소스의 접속 정보를 데이터셋의 요청과 함께 한 문서에 적어요. 프리셋은 이번에 쓰지 않아요.

## 2. 소스 정의의 모양

### 2.1 공통 필드
| 필드 | 뜻 |
|---|---|
| `version` | 문서의 스키마 버전이에요(구현됨, [ADR-0075](adr/0075-definition-id-grammar-and-schema-version.md)). 지금은 `1`이고 없으면 1로 읽어요. 모르는 필드는 지금처럼 거절해요. 정의 저장소의 리비전이 가진 `schema_version`과의 관계는 미결이에요(12절) |
| `id`, `name` | 소스의 id(아래 문법)와 사람이 읽는 이름이에요 |
| `adapter` | `file`, `http`, `postgres` |
| `connection` | 어댑터마다 달라요(2.2부터 2.4) |
| `datasets` | 데이터셋의 목록. 데이터셋의 `id`는 소스 안에서 하나뿐이에요 |
| `disabled` | `true`이면 이 소스의 실행을 막아요. 소스의 삭제는 이번에 없어요. 삭제의 뜻(객체를 닫는 마지막 실행, 상태의 정리)은 뒤에 정해요 |

**id의 문법**(구현됨, [ADR-0075](adr/0075-definition-id-grammar-and-schema-version.md)). 영문 소문자로 시작하고 영문 소문자, 숫자, `_`, `-`만 쓰고 64자까지예요. 소스, 데이터셋, 매핑, 정제, 피드, 링크 정의의 id에 모두 적용해요. 비어 있거나 어긋나면 정의 오류예요. 손으로 쓴 id에 흔한 하이픈(`src-cmdb`)은 허용해요. 도구가 만드는 id(`-emit-source`)는 이미 이 꼴이에요. 어긋나는 옛 id를 고치면 상태는 소스 id와 데이터셋 id를 키로 하므로 옛 상태와 이어지지 않아요(객체가 다시 만들어지고 옛 객체는 아무도 닫지 않아요). 안내서에 적었어요.

**스키마 버전.** 소스와 매핑 문서에 `"version": 1`을 둬요. 버전이 오르면 읽는 코드가 옛 버전을 읽는 처리와 새 버전을 읽는 처리를 구분해요. 스키마 버전이 없는 옛 문서는 버전 1이에요.

데이터셋의 필드는 지금 코드의 것(`kind`, `receive_mode`, `parser`, `natural_key`, `deletion`, `columns` 등)을 그대로 두고 아래를 더해요.

| 필드 | 뜻 |
|---|---|
| `receive_mode` | 데이터셋의 수집 방식이에요. 지금 값은 `snapshot`(전체 수집)뿐이에요 |
| `parser` | 파일 소스에만 적어요. DB와 API의 데이터셋이 적으면 정의 오류예요(레코드가 이미 JSON 객체라서 파싱을 건너뛰어요) |
| `columns[].paths` | 레코드 안의 경로(3.2). 지금처럼 `name`이 기본 경로예요 |
| `table`, `sql` | PostgreSQL의 데이터셋만. 둘 중 하나예요(2.3) |
| `request`, `records_path`, `pagination` | HTTP의 데이터셋만(2.2) |
| `post_reads` | HTTP의 데이터셋만. `true`이면 `request.method`로 `POST`를 쓸 수 있어요(7.1). 기본은 `false`예요 |

### 2.2 HTTP 소스
```json
{
  "version": 1, "id": "src_inventory_api", "name": "자산 관리 API", "adapter": "http",
  "connection": {
    "base_url": "https://inventory.example.test/api/v2",
    "ca_pem": "-----BEGIN CERTIFICATE-----…", "timeout_seconds": 30, "requests_per_second": 5,
    "headers": {"Accept": "application/json"},
    "auth": {"kind": "token_request", "credential": {"secret_id": "sec_inventory"},
             "request": {"path": "/auth/token", "format": "json",
                         "body": {"username": "{{credential.username}}", "password": "{{credential.password}}"}},
             "token_path": "token", "send_header": "Authorization", "send_prefix": "Bearer "}
  },
  "datasets": [{
    "id": "ds_servers",
    "request": {"method": "GET", "path": "/servers", "query": {"status": "active"}},
    "records_path": "items",
    "pagination": {"kind": "page", "page_param": "page", "size_param": "pageSize", "size": 500, "first_page": 0, "total_path": "pageInfo.totalCount"},
    "natural_key": ["serial"],
    "columns": [{"name": "serial"}, {"name": "rack", "paths": ["location.rack"]}]
  }]
}
```
- `auth.kind`는 `none`, `bearer`, `basic`, `header`(헤더 이름과 접두어), `token_request`예요. 위의 예시에 없는 종류의 필드 이름(`bearer`, `basic`, `header`, `offset`, `cursor`, `next_url`, `link_header`)은 정의의 확장 단계의 결정 기록이 정해요(미결). `token_request`는 본문 형식(`json`이나 `form`)으로 토큰을 받아 와요. 받은 토큰은 값을 래핑한 타입에 넣어 한 실행 안에서만 기억하고, 401이면 한 번 다시 받아요. 토큰 주소는 `base_url`과 같은 호스트여야 해요. 다른 호스트의 토큰 주소(OAuth2의 대부분)는 안 돼요(10절).
- 플레이스홀더 `{{credential.<필드>}}`는 인증 설정의 값 자리에서만, 문자열 치환이 아니라 값 단위로 바뀌어요. 값이 정확히 플레이스홀더인 문자열 값만 바뀌어요. 주소(경로, 질의)에는 둘 수 없어요([정의의 쓰기, 시크릿, 경로, 화면](source-editing.md) 3절).
- `pagination.kind`는 `none`, `page`, `offset`, `cursor`(표지를 싣는 자리를 `query`나 `body`로 적어요), `next_url`(본문의 다음 주소. 같은 출처만), `link_header`예요.
- 소스가 전체 개수를 주면(`total_path`) 수집한 수와 견줘요. 다르면 불완전한 수집이에요. 페이지를 넘기는 사이에 소스가 바뀌어 겹치거나 빠진 것을 잡으려는 거예요.
- 응답은 JSON만 읽어요. `connection.headers`에는 쓸 수 없는 헤더가 있어요(8절).
- TLS 확인을 끄는 길은 없어요(8절, [ADR-0091](adr/0091-egress-defence-in-one-unit-adapter-egress.md)). 사설 인증 기관의 서버는 `ca_pem`으로 받고, 그러면 그 인증서만 믿어요. `connection.tls_verify`라는 필드는 없어요.
- 데이터셋의 `request.method`는 `GET`이 기본이에요. `POST`로 읽는 데이터셋은 `"post_reads": true`로 밝혀야 해요(7.1).

### 2.3 PostgreSQL 소스
```json
{
  "version": 1, "id": "src_cmdb", "adapter": "postgres",
  "connection": {"host": "cmdb.example.test", "port": 5432, "database": "cmdb",
                 "credential": {"secret_id": "sec_cmdb"}, "sslmode": "verify-full", "ca_pem": "…",
                 "statement_timeout_seconds": 60},
  "datasets": [
    {"id": "ds_servers", "table": "inventory.servers", "natural_key": ["serial"], "columns": [{"name": "serial"}, {"name": "rack_name"}]},
    {"id": "ds_vm_hosts", "sql": "SELECT vm_id, host_id FROM placement WHERE retired_at IS NULL", "natural_key": ["vm_id"], "columns": [{"name": "vm_id"}, {"name": "host_id"}]}
  ]
}
```
- 계정 이름과 비밀번호는 시크릿(`password` 종류)에서 와요. 비밀번호 없는 접속은 정의 오류예요. `host`는 이름이나 IP 하나이고 쉼표, `/`, `@`가 든 값은 거절해요. 클라이언트 인증서와 키는 받지 않아요(개인 키가 정의에 들어가기 때문이에요). `ca_pem`은 정의에 직접 적을 수 있어요.
- `sslmode`는 `verify-full`이나 `verify-ca`예요. 확인을 낮추는 값(`require`, `allow`, `prefer`)은 받지 않고, 암호화 없는 `disable`은 루프백이거나 `-allow-insecure-sources`일 때만 받아요(8절. 어댑터는 `Dialer.PlaintextAllowed`로 물어요). 이 값을 바꾸는 저장은 접속 변경이에요.
- `table`이면 서버가 컬럼 목록으로 `SELECT`를 만들어요(그 데이터셋은 뒤에 `keyed_read` 지원 기능을 저절로 가져요). `sql`이면 사용자가 쓴 문장 하나예요(7.2).
- **값을 문자열로 바꾸는 규칙.** 모든 값은 문자열이에요.

| 컬럼의 타입 | 글로 |
|---|---|
| NULL | 값 없음 |
| 불리언 | `true`, `false` |
| 정수와 `numeric` | 십진 표기(자릿수를 잃지 않아요. 2의 53제곱을 넘는 정수도 그대로) |
| `timestamptz` | RFC 3339(UTC) |
| 시간대 없는 `timestamp` | 시간대 없이 |
| 날짜 | `YYYY-MM-DD` |
| `json`, `jsonb`, 배열 | JSON 그대로(컬럼 경로가 그 안쪽을 따라가요) |
| 문자열 계열 | 글 |
| 이진(`bytea` 등) | 선언하면 정의 오류 |

### 2.4 파일 소스와 입력 폴더
정의에는 경로가 아니라 **파일 이름**만 적어요: `"connection": {"files": {"ds_servers": "servers.csv"}}`. 서버는 **입력 폴더** 하나를 가져요(`-source-inbox`, [정의의 쓰기, 시크릿, 경로, 화면](source-editing.md) 5절).

- 이름에 경로 구분자를 받지 않아요. 여는 순간 보통 파일인지와 실제 위치가 입력 폴더 안인지를 확인해요(표준 라이브러리의 폴더에 묶인 열기를 써요).
- 입력 폴더는 데이터 폴더와도, 마스터 키가 있는 폴더와도 겹칠 수 없어요. 위치는 서버가 시작할 때 출력하고 화면에 보여요.
- 소스 탐색이 입력 폴더의 파일 목록을 줘요. 명령줄의 `-input 데이터셋=파일`은 그대로이고, 정의의 `files`와 둘 다 있으면 `-input`이 이겨요.
- 읽는 동안 파일이 바뀌었으면(크기나 고친 때) 불완전한 수집이에요.
- 이유: 정의에 임의의 경로를 적게 하면 서버가 읽을 수 있는 모든 파일을 소스로 읽을 수 있어요.

### 2.5 매핑 문서의 쓰임
매핑은 소스에 딸리지 않아요. 문서 하나가 `refinements`, `feeds`, `links`를 가져요. 새로 생기는 것은 문서의 `version`과 `id`(문법은 2.1과 같아요), 그리고 데이터셋을 `<소스 id>.<데이터셋 id>`로 가리키는 `input`(정제)과 `dataset`(피드)이에요. 정제의 결과는 정제 id로 가리켜요. 링크 정의는 어느 매핑 문서의 피드든 id로 가리켜요.

```json
{
  "version": 1, "id": "map_compute",
  "refinements": [{"id": "rf_active_servers", "input": "src_cmdb.ds_servers", "ops": […]}],
  "feeds": [
    {"id": "fd_servers", "dataset": "rf_active_servers", "object_type": "PhysicalServer", …},
    {"id": "fd_racks", "dataset": "src_inventory_api.ds_racks", "object_type": "Rack", …}
  ],
  "links": [{"id": "ld_server_rack", "link_type": "MOUNTED_IN", "from": {"feed": "fd_servers", "columns": ["rack_name"]}, "to": {"feed": "fd_racks", "columns": ["name"]}, "condition": "same_key"}]
}
```

그 밖의 필드는 지금과 같아요. 한 소스만 읽는 옛 매핑 파일(id가 없는 것)은 명령줄이 `-source`의 id를 데이터셋 앞에 붙여 읽고, 저장소에 적용할 때의 매핑 id는 그 소스 id와 같게 해요([정의의 쓰기, 시크릿, 경로, 화면](source-editing.md) 1.5). 자세한 것은 [수집 서비스와 실행](ingestion-service.md) 3절이에요.

## 3. 변수와 경로의 문법

### 3.1 변수
요청 틀(`path`, `query`, `body`)과 SQL에서 `{이름}`은 변수 자리예요. **이번에 정해진 변수는 없으므로 `{`나 `}`가 든 값은 정의 오류예요.** 문자 그대로의 중괄호는 `{{`와 `}}`로 적어요. 증분의 표지, 키로 단건 읽기, 앞 요청의 결과가 나중에 이 자리에 들어와요. 지금 문법을 정해 두면 그때 옛 정의의 뜻이 바뀌지 않아요. (플레이스홀더 `{{credential.…}}`는 인증 설정의 값에만 있고 이 변수와 별개예요.)

### 3.2 컬럼 경로
컬럼의 `paths`는 **대안의 목록**이라서 앞에서부터 처음 값이 있는 경로를 써요. 경로 하나는 점으로 내려가고(`location.rack`) 배열은 순번으로 가리켜요. 이름에 점이 든 키는 JSON에서 `"paths": ["labels[\"app.name\"]"]`처럼 경로 안에서 따옴표로 적어요.

| 표기 | 뜻 | 지금 |
|---|---|---|
| `a.b` | `a` 아래의 `b` | 지원 |
| `labels["app.name"]` | `labels` 아래의 이름에 점이 든 키 `app.name` | 지원 |
| `a.0` | 배열의 첫째(지금 코드와 안내서의 표기 그대로예요) | 지원 |
| `[*]` | 배열을 행으로 펼치기 | 표기만 정함. "아직 지원하지 않음"의 정의 오류 |
| `[?이름=값]` | 조건으로 요소 고르기 | 표기만 정함. "아직 지원하지 않음"의 정의 오류 |

값이 스칼라가 아니거나 null이면 값 없음이에요(지금 코드와 같아요). 순번은 소스가 순서를 보장하지 않으면 틀려요. 그 경우를 `[?이름=값]`이 풀 예정이에요.

## 4. 이름만 정해 둔 필드
지금은 받지 않고(적으면 모르는 필드로 거절) 뒤에 같은 이름으로 들어와요.

| 자리 | 이름 |
|---|---|
| 데이터셋 | `keyed_read`, `marker`, `event_time`, `follow`, `row_select`(지금의 `records_path`가 그 일부), `redact` |
| 컬럼 | `sensitivity` |
| 소스 | `endpoints`(토큰 주소처럼 호스트가 둘 이상일 때), `protection`, `expected_interval`, `preset` |

`status`도 소스의 필드로 뒤에 와요. 지금 소스의 읽기 응답에는 마지막 실행의 참조(`last_sync_run`)만 있어요.

## 5. 지원 기능
소스의 **지원 기능**(`capabilities`)은 역할의 권한(`edit_definitions` 등)과 다른 말이에요. **사용자가 적지 않아요.** 서버가 어댑터와 데이터셋의 설정에서 계산해서 소스의 읽기 응답에 데이터셋마다 `capabilities`로 내요. 지금 값은 `full_read`뿐이에요. PostgreSQL의 `table` 데이터셋은 뒤에 `keyed_read`를 저절로 가져요. 사용자가 적게 하면 소스가 못 하는 일을 한다고 선언할 수 있어서예요. 어댑터와 설정에서 낼 수 있는 지원 기능의 목록은 `GET /v1/adapters`가 줘요.

## 6. 어댑터의 포트

### 6.1 이번에 구현하는 셋과 뒤에 붙을 셋
포트의 이름은 [ADR-0008](adr/0008-ingestion.md)의 것을 써요(결정 2의 메서드 6개와 [수집 설계](ingestion.md) 3.2의 `keyed_read`). 어댑터는 **구현한 인터페이스만** 가져요. 쓰기 메서드는 없어요.

| 포트 | 하는 일 | 이번 |
|---|---|---|
| 전체 수집(`fetch_all`) | 지금의 `Fetcher`를 넓힌 것이에요. 레코드의 순서와 완전성을 내요(**예정(ADR-0086)에서** 페이지의 흐름과 이어 읽기 토큰이에요) | 구현 |
| 점검(`check`) | 접속, 인증, 권한을 확인해요. 항목별로 결과를 내요(**예정(ADR-0086)에서** 첫 페이지를 실제로 읽어요) | 구현 |
| 소스 탐색(`inspect`) | 구조(테이블, 컬럼, 기본 키)와 샘플을 읽기 전용으로 내요. 양에 상한이 있어요. "미리 보기"는 이 포트가 아니라 서버가 가진 레코드로 하는 정의 미리 보기예요([정의의 쓰기, 시크릿, 경로, 화면](source-editing.md) 8절) | 구현 |
| 증분 수집(`fetch_since`) | `marker` 뒤로 바뀐 것 | 뒤에 |
| 이어서 수집(`follow`) | 소스 커서부터 이벤트 | 뒤에 |
| 키로 단건 읽기(`keyed_read`) | 객체 하나의 키로 그 레코드만 | 뒤에 |

점검과 소스 탐색은 시간 한도가 있는 동기 요청이에요. 점검이 PostgreSQL에서 하는 일은 7.2에 있어요.

### 6.2 레코드의 전달 형식
전체 수집의 출력은 바이트가 아니라 **레코드의 순서와 완전성**이에요(**예정(ADR-0086)에서 바뀌어요**: 페이지의 흐름이에요. 페이지마다 이어 읽기 토큰과 끝까지 읽었는지를 알려요. [수집의 규모 설계](ingestion-at-scale-identity.md) 6절). 레코드는 JSON 객체 하나와 선택 필드(소스의 위치와 소스의 시각)예요. 이번에는 위치만 채워요.

- 지금의 `Capture`(데이터셋, 바이트, 해시, 완전성, 이유)에 이 전달 형식을 더해요. 이 모양이면 파싱을 건너뛰고 컬럼 경로만 적용해요. 파일 어댑터는 바이트를 그대로 내고 파서를 거쳐요.
- 완전성은 "끝까지 갔는지"와 이유예요. 페이지 수, 바이트, 시간, 요청 수의 한도를 넘거나 전체 개수와 다르면 불완전해요. 불완전한 수집은 지금처럼 쓰이지 않고(원본 레코드를 닫지 않아요) 이유가 실행 기록에 남아요.
- 문제 목록의 위치는 파일에서는 "몇째 줄"이고, DB와 API에서는 "몇째 페이지의 몇째 레코드"와 자연 키예요.
- 값은 모두 글이에요(2.3의 규칙).

### 6.3 조립
어댑터는 조립 지점(`cmd.praxium`)에서 "종류 이름 → 여는 함수"로 등록해요. 수집 서비스는 어댑터를 가져오지 않고 등록된 것을 이름으로 불러요. 어댑터끼리도 가져오지 않아요. REST 어댑터의 조립에는 시크릿을 복호화하는 포트가 없어요([정의의 쓰기, 시크릿, 경로, 화면](source-editing.md) 3절).

## 7. 읽기 전용: 강제하는 것과 못 하는 것
"읽기 전용"은 이 코드가 강제하는 것과 소스 쪽 계정이 지키는 것을 갈라서 써요. 둘을 섞어 말하지 않아요.

### 7.1 HTTP
| | 내용 | 확인 |
|---|---|---|
| 강제해요 | 기본은 GET이에요. POST는 데이터셋이 `post_reads`로 밝혔을 때와 토큰 발급 요청에만 써요. 정의는 `GET`과 `POST`만 받고(다른 메서드는 정의 오류) 요청을 만드는 코드는 한 곳이며 그곳이 두 메서드만 써요 | 테스트(정의 오류와 요청 조립) |
| 강제해요 | POST 데이터셋은 재시도하지 않아요. 실행 기록과 화면에 매번 "POST로 읽는다"를 보여요 | 테스트 |
| 못 해요 | 소스가 GET에 부작용을 두었는지, `post_reads`가 정말 읽기인지 | 소스 쪽 계정의 권한이 경계예요 |
| 약해져요 | [ADR-0008](adr/0008-ingestion.md) 결정 1과 15의 첫째 방어선(프리셋의 요청 허용 목록)이 없어요. 사용자가 요청을 직접 적어요 | 그래서 소스의 등록과 접속 변경은 `edit_definitions`와 `run_sources`를 둘 다 가진 주체의 일이에요(8절) |

### 7.2 PostgreSQL
**실행마다** 새 연결을 맺고, 읽기 전용 트랜잭션(`BeginTx(ReadOnly)`)에서 **준비한 문장 하나**만 실행해요. 문장 시간(`statement_timeout`), 잠금 시간(`lock_timeout`), 트랜잭션 안의 대기 시간(`idle_in_transaction_session_timeout`)을 걸어요. 사용자의 문장은 하나만 받아요(준비한 문장). 큰 결과를 한꺼번에 메모리에 올리지 않아요. 방법은 PostgreSQL 어댑터의 단계가 실제 드라이버로 확인해 정해요. **예정(ADR-0086)에서** 큰 결과는 자연 키의 순서로 끊어 읽고 이어 읽기 토큰은 마지막 키예요. `sql`은 준비 단계의 결과 설명(행을 돌려주는가)과 첫 낱말(`SELECT`, `WITH`, `TABLE`, `VALUES`)로 걸러요.

문서와 화면에 이 문장을 그대로 써요. **"프락시움은 읽기 전용 트랜잭션에서 문장 하나만 실행한다. 보통의 쓰기 문장은 막히지만 함수 호출과 `COPY TO`는 막지 못한다. 경계는 소스 쪽의 읽기 전용 계정이다."**

- 점검은 그 계정이 쓰기 권한을 가졌는지 조사해 경고해요. 조사하지 못하면 "확인했다"가 아니라 "확인하지 못함"이라고 써요.
- 연결을 맺는 순간의 실제 IP 검사와 거절 목록(8절)을 HTTP와 같은 다이얼러로 PostgreSQL 접속에도 걸어요.
- 접속 설정을 서버의 환경에서 읽지 않아요. 접속 설정을 손으로 채워서 환경 변수, 서비스 파일, `.pgpass`를 보지 않아요.
- 오류는 8절의 분류로 바꿔요. 예외 하나: 사용자가 쓴 SQL의 문법 오류와 없는 테이블과 컬럼(SQLSTATE 42류)은 요청한 사람에게 그 응답에서만 서버의 문장과 위치를 보여요. 실행 기록과 로그에는 분류만 남겨요.

## 8. 바깥으로 나가는 요청의 묶기
소스를 등록할 수 있는 사람은 서버가 접속할 수 있는 모든 내부 주소에 GET을 보낼 수 있고 응답의 일부를 볼 수 있어요. 그래서 **소스의 등록과 접속 변경은 `edit_definitions`와 `run_sources`를 둘 다 가진 주체(지금의 역할 표로는 `editor`와 `operator`를 함께 가진 주체)의 일이고 감사에 남아요.** 이 문장을 문서와 화면에 둬요. 아래는 그 위험을 줄이는 묶음이에요.

묶기의 코드는 HTTP 어댑터 안이 아니라 따로 된 한 곳에 두고 정책(거절 목록, 허용하는 스킴, 시간 한도)을 인자로 받아요. PostgreSQL 접속이 같은 코드를 쓰고, 뒤에 서버가 바깥으로 요청을 보내는 다른 기능이 생겨도 같은 코드를 지나요.

| 구분 | 내용 |
|---|---|
| 경로 | `/`로 시작해요. `//`, `\`, `?`, `#`, 스킴이 없어요. 조립한 주소의 스킴, 호스트, 포트가 `base_url`과 같은지 다시 확인해요 |
| 리디렉션 | 따르지 않아요(3xx는 오류). 다음 페이지의 주소(본문, `Link` 헤더)는 같은 출처일 때만 따라요 |
| 실제 IP | **이름을 스스로 한 번 풀고, 풀린 모든 주소를 검사하고, 검사한 IP 글자로 연결해요**(다시 풀지 않아요. 이름 풀이가 연결마다 다른 IP를 주는 공격을 막아요). 하나라도 거절 목록에 있으면 전체를 거절해요. 거절 목록: 링크 로컬, 메타데이터 주소, `0.0.0.0`과 `0.0.0.0/8`, 멀티캐스트, 예약 대역과 브로드캐스트, IPv4가 든 IPv6(매핑, 호환, 번역, NAT64, 6to4)의 속 주소, 터널 주소(Teredo), 서버 자신의 API 주소. 서버 플래그(`-source-deny-hosts`)로 더해요. 사설 주소와 루프백은 기본으로 허용해요. 사설과 루프백을 막고 예외만 여는 엄격한 정책(`DenyPrivate`)도 있어요 |
| 프록시 | 쓰지 않아요. 환경 변수를 읽지 않는 전송 계층을 새로 만들어요 |
| 헤더 | `connection.headers`에 `Host`, `Authorization`, `Cookie`, `Proxy-*`, `Accept-Encoding`, `Content-Length`, `Transfer-Encoding`, `Connection`을 쓸 수 없어요. 인증은 `auth`로만 해요 |
| 전송 계층 | `https`가 기본이에요. `http`(와 PostgreSQL의 `sslmode=disable`)는 루프백이거나 `-allow-insecure-sources`일 때만이에요. TLS는 1.2 이상이고 인증서와 이름을 언제나 확인해요. **확인을 끄는 길은 없어요**(ADR-0091이 ADR-0058의 `tls_verify: false`와 `-allow-unverified-source-tls`를 거두었어요) |
| 오류 | 표준 라이브러리와 드라이버의 문장을 그대로 쓰지 않아요. 이름 풀이, 연결, TLS, 인증, 권한, 시간 초과, 응답 형식으로 분류해요. 토큰 발급 응답의 본문을 오류에 넣지 않아요 |
| 로그 | 값을 래핑한 타입에서 꺼내는 자리는 어댑터의 요청 조립 한 곳이에요(테스트로 고정). 요청과 응답을 로그에 찍는 코드가 없어요 |

`-allow-`로 시작하는 새 플래그는 `-allow-insecure-sources` 하나이고 [ADR-0064](adr/0064-security-basics-report-channel-pinned-actions-and-database-tls.md)의 데이터베이스 TLS 플래그와 같은 방식(기본은 꺼짐)이에요. `-source-deny-hosts`는 거절 목록에 더하는 플래그예요. 이것은 ADR-0018 결정 9의 "소스별 예외, 승인, 기한"이 아니라 정의의 명시와 서버 플래그로 여는 방식이고 기한이 없어요.

**구현(`adapter.egress`, [ADR-0091](adr/0091-egress-defence-in-one-unit-adapter-egress.md)).** 위 표의 실제 IP, 프록시, 전송 계층, 오류, 리디렉션, 같은 출처의 규칙은 이 유닛이 해요. 어댑터는 `Dialer`와 `Client`와 `TLSConfig`를 여기서 받고 자기 연결을 따로 만들지 않아요. 아직 이 유닛을 부르는 어댑터와 조립부가 없어서 `-allow-insecure-sources`와 `-source-deny-hosts` 플래그는 예정이에요. 이 유닛이 정한 것 가운데 위 표와 다른 곳은 둘이에요. (1) TLS 확인을 끄는 길이 없어요. (2) 리디렉션은 기본으로 따르지 않고(3xx는 오류), 켜도 같은 출처의 것만 따라요. 다른 출처로의 리디렉션은 다시 검사하는 대신 거절해요(자격 증명이 든 헤더가 다른 곳으로 가지 않게요). 속도 제한과 재시도는 이 유닛이 아니라 HTTP 어댑터의 일이에요.

## 9. 한도
숫자는 [기본값 표](defaults.md)에 "예정"으로 있어요. 넘으면 불완전한 수집이에요.

- 페이지 수(기본 1,000), 압축을 푼 뒤의 바이트(한 페이지의 바이트와 데이터셋 전체의 바이트), 실행 전체의 시간과 요청 수.
- 요청의 속도(초당 5회)와 재시도(GET의 429와 5xx에서 `Retry-After`를 지켜 세 번까지, 401과 403은 곧바로 멈춰요).
- 데이터셋마다 압축을 푼 뒤 20 MiB와 200,000행(파일의 지금 한도와 같은 크기에서 시작해요. 기본값 표에 "예정, 가정"으로 있어요). PostgreSQL의 문장 시간은 60초예요.
- 동시에 도는 수집은 4개이고, 소스 하나의 수집은 겹치지 않아요([수집 서비스와 실행](ingestion-service.md) 5절).

## 10. HTTP의 선언만으로 되는 것과 안 되는 것
선언만으로 되는 것은 **평평한 목록까지**예요. 소스를 붙이기 전에 이 표로 가늠해요.

| 경우 | 되나요 |
|---|---|
| 토큰을 헤더로 보내는 평평한 목록(페이지 번호, 오프셋, 전체 개수) | 돼요 |
| 로그인 요청으로 토큰을 받는 API(본문이 JSON이나 폼, 같은 호스트) | 돼요 |
| 다음 페이지의 주소를 본문이나 `Link` 헤더로 주는 API(같은 출처) | 돼요 |
| 삭제를 상태 값으로 알리는 소스 | 정제의 `filter`로 걸러서 닫아요(ADR-0052). 돼요 |
| OAuth2 클라이언트 자격 증명(토큰 주소가 다른 호스트) | 안 돼요 |
| 키를 질의 인자로 받는 API | 안 돼요(시크릿을 주소에 둘 수 없어요. 의도한 제한이에요) |
| POST 본문에 커서를 넣는 검색 API | 커서를 `body`에 싣는 자리를 적으면 돼요 |
| 레코드가 배열이 아니라 "키 → 객체"의 맵 | 안 돼요 |
| 레코드 안의 배열을 행으로 펼치기, 조건으로 요소 고르기 | 안 돼요(`[*]`, `[?…]`는 표기만 정했어요) |
| 앞 요청의 결과가 필요한 요청(id 목록을 본문에 넣는 일괄 조회) | 안 돼요 |
| 관계가 별도 호출로만 나오는 경우 | 호출이 평평한 (부모, 자식) 행을 주면 값만 붙이는 피드와 링크 정의로 될 수 있어요(돌려 보지 않았어요) |
| 값이 배열이나 객체인 컬럼 | 값 없음이 돼요 |
| 값의 시각 | 받지 않아요. 모든 값의 시각은 수집한 때예요 |
| PostgreSQL | `sql`이 조인, 펼치기, 피벗을 대신하므로 위의 대부분이 돼요 |

[수집 설계](ingestion.md) 12절의 "선언만으로 되는 것은 단순한 요청까지"를 이 표가 구체로 적은 거예요. 까다로운 소스는 어댑터가 따로 필요해요.

## 11. 샘플 소스
DB와 API는 붙일 곳이 있어야 해 볼 수 있어요. 합성 세계(작은 세계)를 읽기 전용 HTTP API로 내는 명령과 PostgreSQL에 넣는 SQL을 내는 명령을 둬요(작업 이름 `praxium sample-source`, 아직 없어요).

- API는 토큰 발급, 페이지 번호와 전체 개수, 본문의 다음 주소를 가진 경로를 함께 내요. 안내서, 독립 검증, 끝에서 끝까지의 테스트가 모두 이것을 써요.
- 그 소스에 맞는 소스 정의와 매핑 정의의 예시를 함께 내요. PostgreSQL용 SQL에는 읽기 전용 역할을 만드는 문장을 넣어요.
- API를 내는 코드는 새 어댑터 유닛에 둬요. 구조 계약의 변경이고 그 단계의 결정 기록이 실어요.
- 테스트의 주소는 문서용으로 예약된 `example.test` 같은 이름만 써요. 특정 제품의 경로나 헤더 접두어를 쓰지 않고, 지어낸 이름으로 같은 모양을 만들어요.

## 12. 알려진 한계와 미결
- 값의 시각은 수집한 때이고, 증분과 스트림이 없어서 소스가 바뀐 것을 받는 길은 전체 읽기뿐이에요.
- 수집한 값에 섞인 시크릿은 지우지 않아요(`redact`는 이름만 정했어요). 소스 탐색의 샘플은 필드 이름이 `password`, `passwd`, `secret`, `token`, `api_key`, `apikey`, `authorization`, `credential`을 포함하면(대소문자 무시) 값을 가리고 가렸다고 적어요. `key` 한 낱말로는 가리지 않아요. 가린 샘플로 정의 미리 보기를 하면 가린 값이 그대로 쓰여요.
- PostgreSQL이 아닌 DB는 없어요. 드라이버는 저장소가 이미 쓰는 것 하나예요.
- `edit_definitions`와 `inspect_sources`를 가진 주체는 저장된 소스의 시크릿으로, 묶인 호스트의 아무 경로에나 `GET`을 보낼 수 있어요(데이터셋을 정의하는 일이 곧 그 일이에요). `post_reads`를 켜거나 `POST` 요청을 바꾸는 저장과 `connection.auth`의 변경은 `run_sources`가 필요해요([정의의 쓰기, 시크릿, 경로, 화면](source-editing.md) 1.3).
- 미결: 문서의 `version`과 정의 저장소의 `schema_version`의 관계(8a 단계의 결정 기록), 인증과 페이지네이션의 종류별 필드 이름(4a 단계의 결정 기록).

## 13. 관련 결정과 요구사항
[ADR-0058](adr/0058-sources-from-db-and-api-worker-ingestion-and-editing-screens.md), [ADR-0008](adr/0008-ingestion.md), [ADR-0064](adr/0064-security-basics-report-channel-pinned-actions-and-database-tls.md). 요구사항: REQ-110, REQ-111, REQ-174(예정, 문장을 이 설계에 맞춰 두었어요), REQ-401, REQ-411, REQ-412, REQ-419, REQ-441, REQ-484, REQ-486부터 REQ-491까지. 앞 문서: [수집 설계](ingestion.md), [파이프라인 설계](pipeline.md).
