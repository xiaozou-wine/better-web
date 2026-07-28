// 用最小桩件验证 019-screen-fingerprint.patch 的辅助函数逻辑。
//
// 存在意义：完整编译 Chromium 需要数小时和数十 GB 依赖，而补丁里的
// 取值、校验与一致性约束是纯逻辑，可以独立验证。这样在真正开始
// 长时间构建之前就能排掉语法错误和边界处理错误。
//
// 构建运行：
//   g++ -std=c++20 -Wall -Wextra -o verify_logic verify_logic.cc && ./verify_logic
//
// 这里的桩件必须与 Chromium 的真实签名保持一致，否则验证没有意义：
//   base::CommandLine::HasSwitch(std::string_view) -> bool
//   base::CommandLine::GetSwitchValueASCII(std::string_view) -> std::string
//   base::StringToInt(std::string_view, int*) -> bool
//   gfx::Size(int, int) / gfx::Rect(int, int, int, int)

#include <algorithm>
#include <cassert>
#include <cstdio>
#include <map>
#include <optional>
#include <string>
#include <string_view>

// ---------- 桩件 ----------

namespace gfx {

class Size {
 public:
  Size() = default;
  Size(int width, int height) : width_(width), height_(height) {}
  int width() const { return width_; }
  int height() const { return height_; }

 private:
  int width_ = 0;
  int height_ = 0;
};

class Rect {
 public:
  Rect() = default;
  explicit Rect(const Size& size)
      : width_(size.width()), height_(size.height()) {}
  Rect(int x, int y, int width, int height)
      : x_(x), y_(y), width_(width), height_(height) {}
  int x() const { return x_; }
  int y() const { return y_; }
  int width() const { return width_; }
  int height() const { return height_; }

 private:
  int x_ = 0;
  int y_ = 0;
  int width_ = 0;
  int height_ = 0;
};

}  // namespace gfx

namespace base {

// 测试用的命令行桩件，只实现补丁用到的两个方法。
class CommandLine {
 public:
  static CommandLine* ForCurrentProcess() { return &instance_; }
  static void ResetForTesting() { instance_.switches_.clear(); }
  static void SetSwitchForTesting(const std::string& key,
                                  const std::string& value) {
    instance_.switches_[key] = value;
  }

  bool HasSwitch(std::string_view name) const {
    return switches_.count(std::string(name)) > 0;
  }
  std::string GetSwitchValueASCII(std::string_view name) const {
    auto it = switches_.find(std::string(name));
    return it == switches_.end() ? std::string() : it->second;
  }

 private:
  std::map<std::string, std::string> switches_;
  static CommandLine instance_;
};

CommandLine CommandLine::instance_;

// 与 base::StringToInt 行为一致：整串必须是合法十进制整数，
// 有多余字符或溢出即失败。
bool StringToInt(std::string_view input, int* output) {
  if (input.empty()) {
    return false;
  }
  size_t idx = 0;
  bool negative = false;
  if (input[0] == '-' || input[0] == '+') {
    negative = input[0] == '-';
    idx = 1;
    if (input.size() == 1) {
      return false;
    }
  }
  long long acc = 0;
  for (; idx < input.size(); ++idx) {
    const char c = input[idx];
    if (c < '0' || c > '9') {
      return false;
    }
    acc = acc * 10 + (c - '0');
    if (acc > 2147483647LL) {
      return false;
    }
  }
  *output = static_cast<int>(negative ? -acc : acc);
  return true;
}

}  // namespace base

namespace switches {
const char kFingerprintScreenWidth[] = "fingerprint-screen-width";
const char kFingerprintScreenHeight[] = "fingerprint-screen-height";
}  // namespace switches

// ---------- 以下为补丁中的实际代码，逐字复制 ----------

namespace {

constexpr int kSyntheticTaskbarHeight = 40;

constexpr int kMinScreenDimension = 320;
constexpr int kMaxScreenDimension = 8192;

std::optional<gfx::Size> SpoofedScreenSize() {
  const base::CommandLine* cmd = base::CommandLine::ForCurrentProcess();
  if (!cmd->HasSwitch(switches::kFingerprintScreenWidth) ||
      !cmd->HasSwitch(switches::kFingerprintScreenHeight)) {
    return std::nullopt;
  }

  int width = 0;
  int height = 0;
  if (!base::StringToInt(
          cmd->GetSwitchValueASCII(switches::kFingerprintScreenWidth),
          &width) ||
      !base::StringToInt(
          cmd->GetSwitchValueASCII(switches::kFingerprintScreenHeight),
          &height)) {
    return std::nullopt;
  }
  if (width < kMinScreenDimension || width > kMaxScreenDimension ||
      height < kMinScreenDimension || height > kMaxScreenDimension) {
    return std::nullopt;
  }
  return gfx::Size(width, height);
}

gfx::Rect SyntheticScreenRect(const gfx::Size& size, bool available) {
  if (!available) {
    return gfx::Rect(size);
  }
  const int usable_height =
      std::max(kMinScreenDimension, size.height() - kSyntheticTaskbarHeight);
  return gfx::Rect(0, 0, size.width(), usable_height);
}

}  // namespace

// ---------- 验证 ----------

namespace {

int g_failures = 0;

void Check(bool ok, const char* what) {
  if (!ok) {
    std::printf("FAIL: %s\n", what);
    ++g_failures;
  }
}

void SetSize(const char* w, const char* h) {
  base::CommandLine::ResetForTesting();
  if (w) {
    base::CommandLine::SetSwitchForTesting(switches::kFingerprintScreenWidth, w);
  }
  if (h) {
    base::CommandLine::SetSwitchForTesting(switches::kFingerprintScreenHeight,
                                           h);
  }
}

void TestValidValuesAccepted() {
  SetSize("1366", "768");
  auto size = SpoofedScreenSize();
  Check(size.has_value(), "合法取值应被接受");
  Check(size->width() == 1366 && size->height() == 768, "取值应原样返回");
}

void TestMissingSwitchesRejected() {
  SetSize(nullptr, nullptr);
  Check(!SpoofedScreenSize().has_value(), "未提供参数时不应伪造");

  // 只给一个维度会让伪造值与真实值混用，产生不存在的宽高比。
  SetSize("1366", nullptr);
  Check(!SpoofedScreenSize().has_value(), "只给宽度时应拒绝");
  SetSize(nullptr, "768");
  Check(!SpoofedScreenSize().has_value(), "只给高度时应拒绝");
}

void TestInvalidValuesRejected() {
  const char* bad[] = {"", "abc", "1366px", "12.5", "-1366", "0", " 1366"};
  for (const char* v : bad) {
    SetSize(v, "768");
    Check(!SpoofedScreenSize().has_value(), "非法宽度应被拒绝");
  }
  // 越界值宁可不伪造，也不要产出比真实值更可疑的尺寸。
  SetSize("100", "768");
  Check(!SpoofedScreenSize().has_value(), "过小的宽度应被拒绝");
  SetSize("99999", "768");
  Check(!SpoofedScreenSize().has_value(), "过大的宽度应被拒绝");
  SetSize("1366", "100");
  Check(!SpoofedScreenSize().has_value(), "过小的高度应被拒绝");
}

void TestBoundaryValuesAccepted() {
  SetSize("320", "320");
  Check(SpoofedScreenSize().has_value(), "下界应被接受");
  SetSize("8192", "8192");
  Check(SpoofedScreenSize().has_value(), "上界应被接受");
}

void TestAvailRectIsSmallerThanScreen() {
  const gfx::Size size(1920, 1080);
  const gfx::Rect full = SyntheticScreenRect(size, /*available=*/false);
  const gfx::Rect avail = SyntheticScreenRect(size, /*available=*/true);

  Check(full.width() == 1920 && full.height() == 1080, "全屏矩形应等于声明尺寸");

  // screen.height == screen.availHeight 意味着没有任务栏，
  // 这正是 CreepJS 的 noTaskbar 判定项。
  Check(avail.height() < full.height(), "可用高度必须小于屏幕高度，否则等于宣告无任务栏");
  Check(avail.width() == full.width(), "底部任务栏不应改变可用宽度");
  Check(avail.height() == 1080 - kSyntheticTaskbarHeight, "应扣除任务栏高度");

  // 底部停靠的任务栏不改变左上角坐标。
  Check(avail.x() == 0 && avail.y() == 0, "可用区域原点应为 (0,0)");
}

void TestAvailRectNeverCollapses() {
  // 极小屏幕上扣除任务栏后不应得到负数或零高度。
  const gfx::Rect avail = SyntheticScreenRect(gfx::Size(320, 320), true);
  Check(avail.height() >= kMinScreenDimension, "可用高度不应塌陷到下界之下");
}

}  // namespace

int main() {
  TestValidValuesAccepted();
  TestMissingSwitchesRejected();
  TestInvalidValuesRejected();
  TestBoundaryValuesAccepted();
  TestAvailRectIsSmallerThanScreen();
  TestAvailRectNeverCollapses();

  if (g_failures == 0) {
    std::printf("全部检查通过\n");
    return 0;
  }
  std::printf("%d 项检查失败\n", g_failures);
  return 1;
}
