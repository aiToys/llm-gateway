#!/usr/bin/env bash
# 运行时探针: 用 curl 跑关键 API,快速验证网关健康 + 管理面核心链路。
# 用法: BASE_URL=http://localhost:8088 ADMIN_EMAIL=admin@demo.com ADMIN_PASS=admin123 ./scripts/smoke.sh
# 退出码 = 失败步数(0 = 全绿)。便于日常改完快速验证 / CI 前置自检。
#
# 设计原则: 每步独立打印 ✓/✗ + 耗时,不因单步失败中断(收集全部结果)。
# 依赖: bash, curl, python3(解析 JSON + 毫秒时间), macOS/Linux 通用。

set -u
BASE_URL="${BASE_URL:-http://localhost:8088}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@demo.com}"
ADMIN_PASS="${ADMIN_PASS:-admin123}"
FAILS=0
PASS=0

green() { printf "\033[32m%s\033[0m" "$1"; }
red()   { printf "\033[31m%s\033[0m" "$1"; }
gray()  { printf "\033[90m%s\033[0m" "$1"; }

# 毫秒时间戳(date %N 在 macOS 不支持,统一用 python3)
now_ms() { python3 -c 'import time;print(int(time.time()*1000))'; }

# json_get 'a.b.c' 从 stdin 读 JSON 取嵌套值
json_get() { python3 -c "import sys,json
try:
    d=json.load(sys.stdin)
    for k in '$1'.split('.'):
        d = d.get(k) if isinstance(d,dict) else None
    print(d if d is not None else '')
except Exception: print('')"; }

# check <描述> <期望子串> <期望HTTP码|空> <curl参数...>
# 期望码为空 → 要求 2xx; 否则要求精确匹配该码。期望子串需出现在响应体里。
check() {
  local desc="$1" expect="$2" want_code="$3"; shift 3
  local t0 t1 code body ok
  t0=$(now_ms)
  body=$(curl -s -w "\n%{http_code}" --max-time 10 "$@" 2>/dev/null)
  code=$(printf '%s' "$body" | tail -n1)
  body=$(printf '%s' "$body" | sed '$d')
  t1=$(now_ms)
  if [ -n "$want_code" ]; then ok=$([ "$code" = "$want_code" ] && echo 1 || echo 0)
  else ok=$([[ "$code" == 2* ]] && echo 1 || echo 0); fi
  if [ -n "$expect" ] && ! printf '%s' "$body" | grep -q "$expect"; then ok=0; fi
  if [ "$ok" = 1 ]; then
    printf "  %s %s %s\n" "$(green '✓')" "$desc" "$(gray "(${code}, $((t1-t0))ms)")"
    PASS=$((PASS+1))
  else
    printf "  %s %s %s\n" "$(red '✗')" "$desc" "$(gray "(${code}, 期望 ${want_code:-2xx})")"
    printf "      %s\n" "$(red "body: $(printf '%s' "$body" | head -c 200)")"
    FAILS=$((FAILS+1))
  fi
}

echo "Smoke test → $BASE_URL"

# 1. 健康检查
check "GET /healthz"            "ok"      ""  "$BASE_URL/healthz"
check "GET /readyz"             "ok"      ""  "$BASE_URL/readyz"
check "GET /metrics (Prometheus)" "llm_"  ""  "$BASE_URL/metrics"

# 2. 公共接口
check "GET /api/public/models (非空)" "model_name" "" "$BASE_URL/api/public/models"

# 3. 管理端登录
LOGIN=$(curl -s --max-time 10 -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASS\"}" \
  "$BASE_URL/api/auth/login")
TOKEN=$(printf '%s' "$LOGIN" | json_get 'token')
if [ -n "$TOKEN" ]; then
  printf "  %s admin 登录拿 token %s\n" "$(green '✓')" "$(gray "(${#TOKEN} chars)")"
  PASS=$((PASS+1))
else
  printf "  %s admin 登录失败: %s\n" "$(red '✗')" "$(printf '%s' "$LOGIN" | head -c 200)"
  FAILS=$((FAILS+1))
fi

AUTH=(-H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json')

# 4. 渠道创建 - 空 name 应 400 + 中文化(验证 binding 脱敏)
check "POST /admin/channels 空 name → 400 中文报错" "Name 不能为空" "400" \
  -X POST "${AUTH[@]}" -d '{"provider":"mock","channel_models":[{"model_name":"smoke-test"}]}' \
  "$BASE_URL/api/admin/channels"

# 5. 渠道创建 - 完整 payload 应成功,记录 id 末尾清理
CID=$(curl -s --max-time 10 -X POST "${AUTH[@]}" \
  -d '{"provider":"mock","name":"smoke-test","channel_models":[{"model_name":"smoke-test-model"}]}' \
  "$BASE_URL/api/admin/channels" | json_get 'data.id')
if [ -n "$CID" ]; then
  printf "  %s 渠道创建成功 %s\n" "$(green '✓')" "$(gray "(id=$CID)")"
  PASS=$((PASS+1))
  curl -s --max-time 10 -X DELETE "${AUTH[@]}" "$BASE_URL/api/admin/channels/$CID" >/dev/null
  printf "  %s 清理测试渠道 %s\n" "$(green '✓')" "$(gray "(deleted)")"
  PASS=$((PASS+1))
else
  printf "  %s 渠道创建失败\n" "$(red '✗')"; FAILS=$((FAILS+1))
fi

# 6. 渠道列表可达
check "GET /admin/channels (列表)" "id" "" -X GET "${AUTH[@]}" "$BASE_URL/api/admin/channels"

# 7. 静态资源(MountSPAs 修复后,public 根文件应作静态资源返回 svg)
check "GET /logo.svg → svg (静态托管)" "svg" "" "$BASE_URL/logo.svg"

echo
printf "%s: %d 通过, %d 失败\n" "$( [ $FAILS = 0 ] && green '结果' || red '结果' )" "$PASS" "$FAILS"
exit "$FAILS"
