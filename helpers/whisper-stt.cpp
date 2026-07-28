// loqui whisper.cpp STT helper (the "whisper" provider). Live on-device mic
// transcription via whisper.cpp in VAD mode; emits our JSONL protocol
// (src/shared/sttHelperProtocol.ts). Adapted from whisper.cpp's examples/stream.
//
//   argv[1] = locale (e.g. "es-CO" → base language "es")
//   argv[2] = model path (optional; else $WHISPER_MODEL, else "ggml-small.bin")
//   built by scripts/build-whisper-stt.sh; stopped by main via SIGTERM.
//
// Emits: {"type":"started"} / {"type":"final","text":..,"language":..}
//      / {"type":"canceled","error":..} / {"type":"stopped"}
// (whisper is per-utterance under VAD, so we emit finals — no word-by-word partials.)
#include "common-sdl.h"
#include "common.h"
#include "common-whisper.h"
#include "whisper.h"

#include <atomic>
#include <chrono>
#include <csignal>
#include <iostream>
#include <mutex>
#include <thread>
#include <cstdio>
#include <cstdlib>
#include <string>
#include <thread>
#include <vector>

static std::atomic<bool> g_running{true};
static void on_signal(int) { g_running = false; }

// "0", "false" or "off" turn a flag off; anything else (or unset) leaves the
// default. Only used for the two whisper flags that a bad GPU driver can make
// fatal — see where they are read.
static bool env_flag(const char * name, bool fallback) {
  const char * v = getenv(name);
  if (!v || !*v) return fallback;
  return !(v[0] == '0' || v[0] == 'f' || v[0] == 'F' || v[0] == 'n' || v[0] == 'N');
}

// Locked because the heartbeat thread below writes here too, and two interleaved
// printf calls would produce one corrupt line the parent cannot parse.
static std::mutex g_emit_mu;

static void emit(const std::string & json) {
  std::lock_guard<std::mutex> lock(g_emit_mu);
  printf("%s\n", json.c_str());
  fflush(stdout);
}

static std::string json_escape(const std::string & s) {
  std::string o;
  for (char c : s) {
    switch (c) {
      case '"': o += "\\\""; break;
      case '\\': o += "\\\\"; break;
      case '\n': o += "\\n"; break;
      case '\r': o += "\\r"; break;
      case '\t': o += "\\t"; break;
      default:
        if ((unsigned char) c < 0x20) { char b[8]; snprintf(b, sizeof(b), "\\u%04x", c); o += b; }
        else o += c;
    }
  }
  return o;
}

// `trim(const std::string&)` is provided by whisper.cpp's common.h/common.cpp.

// A sign of life while whisper_full runs, which is the ONLY thing that takes
// real time here. On a machine with no GPU backend a single pass costs 10-15 s
// (measured: whisper small, Ryzen 5 3500U) against under a second on Metal, so
// the parent cannot tell "still transcribing" from "wedged" by waiting a fixed
// number of seconds. It stops on SILENCE instead, and these lines reset that.
// "info" is not an event type, so the parser ignores them (sttHelperProtocol.ts).
//
// ON A FIXED CADENCE, not on whisper's progress callback alone: observed, that
// callback fired only at 0 % and 100 %, which for a 15 s pass is a 15 s gap —
// longer than the parent's patience, so the tail would be cut off again for a
// new reason. A thread that ticks while a pass is in flight cannot have that
// problem, whatever the audio length or the backend.
static std::atomic<bool> g_transcribing{false};

static void on_progress(struct whisper_context *, struct whisper_state *, int progress, void *) {
  emit("{\"type\":\"info\",\"stage\":\"transcribing\",\"progress\":" + std::to_string(progress) + "}");
}

static void heartbeat_loop() {
  while (g_running) {
    std::this_thread::sleep_for(std::chrono::seconds(2));
    if (g_transcribing) emit("{\"type\":\"info\",\"stage\":\"transcribing\"}");
  }
}

// RAII so an early `break` or an exception cannot leave the flag stuck on.
struct TranscribingScope {
  TranscribingScope() { g_transcribing = true; }
  ~TranscribingScope() { g_transcribing = false; }
};

// whisper invents confident text out of near-silence, and marks non-speech with
// wrappers: [BLANK_AUDIO], [Música], *música*, (viento)… The old check compared
// against three exact strings, so "*música*" sailed through and would have been
// pasted into the user's document. Anything WHOLLY wrapped in [], *, () or ♪ is
// a marker, never something a person said.
static bool is_noise_marker(const std::string & t) {
  if (t.empty()) return true;
  const char a = t.front(), b = t.back();
  if ((a == '[' && b == ']') || (a == '*' && b == '*') ||
      (a == '(' && b == ')') || (a == '\xe2')) {
    return true; // the last covers the ♪ musical-note marker (UTF-8 lead byte)
  }
  return false;
}

int main(int argc, char ** argv) {
  // Signals remain the POSIX path, but they are NOT portable: Windows has no
  // SIGTERM, and Node's kill() there is TerminateProcess — the process dies
  // instantly, so the tail flush below would never run and every dictation would
  // lose its ending again. stdin works identically on both systems.
  signal(SIGINT, on_signal);
  signal(SIGTERM, on_signal);

  // Stop on a "stop" line, or on EOF — which also covers the parent dying, so a
  // crashed app can't leave this holding the microphone.
  std::thread([] {
    std::string line;
    while (std::getline(std::cin, line)) {
      if (line == "stop") break;
    }
    g_running = false;
  }).detach();
  std::thread(heartbeat_loop).detach();
  ggml_backend_load_all();

  const std::string locale = argc > 1 ? argv[1] : "en";
  const std::string lang = locale.substr(0, locale.find('-')); // "es-CO" -> "es"
  std::string model = argc > 2 ? argv[2] : "ggml-small.bin";
  if (argc <= 2) { const char * env = getenv("WHISPER_MODEL"); if (env) model = env; }

  // TWO DIFFERENT THINGS, previously conflated into one 10 s number:
  //  - kBufferMs: how much audio the ring buffer keeps. It bounds what can ever
  //    be recovered, so it must cover a whole dictation. At 10 s, speaking for
  //    16 s without a pause silently lost the first 6 — the audio was already
  //    overwritten before anything asked for it.
  //  - length_ms: the VAD cadence below. Unchanged.
  // 5 minutes at 16 kHz mono float32 is ~19 MB, which is a fine trade for never
  // truncating what someone actually said.
  const int kBufferMs = 300000;
  const int length_ms = 10000;
  const float vad_thold = 0.6f, freq_thold = 100.0f;
  const int n_samples_30s = (int)((1e-3 * 30000.0) * WHISPER_SAMPLE_RATE);

  audio_async audio(kBufferMs);
  if (!audio.init(-1, WHISPER_SAMPLE_RATE)) {
    emit("{\"type\":\"canceled\",\"error\":\"audio init failed (mic permission?)\"}");
    return 1;
  }
  audio.resume();

  if (lang != "auto" && whisper_lang_id(lang.c_str()) == -1) {
    emit("{\"type\":\"canceled\",\"error\":\"unknown language: " + json_escape(lang) + "\"}");
    return 1;
  }

  // Overridable so a machine that crashes can be bisected WITHOUT a new build.
  // Both matter on Windows, where the GPU backend is Vulkan and the driver may
  // be anything: an AMD iGPU reporting "fp16: 0" cannot run flash attention,
  // which is implemented in half precision. Env vars, because the helper
  // inherits the app's environment and a user can set one in a shell.
  whisper_context_params cparams = whisper_context_default_params();
  cparams.use_gpu = env_flag("LOQUI_WHISPER_GPU", true);
  cparams.flash_attn = env_flag("LOQUI_WHISPER_FLASH", true);
  fprintf(stderr, "loqui: use_gpu=%d flash_attn=%d (LOQUI_WHISPER_GPU / LOQUI_WHISPER_FLASH)\n",
          (int) cparams.use_gpu, (int) cparams.flash_attn);
  whisper_context * ctx = whisper_init_from_file_with_params(model.c_str(), cparams);
  if (!ctx) {
    emit("{\"type\":\"canceled\",\"error\":\"failed to load model: " + json_escape(model) + "\"}");
    return 2;
  }

  std::vector<float> pcmf32(n_samples_30s, 0.0f);
  std::vector<float> pcmf32_new(n_samples_30s, 0.0f);

  emit("{\"type\":\"started\"}");

  auto t_last = std::chrono::high_resolution_clock::now();

  while (g_running) {
    if (!sdl_poll_events()) break; // SDL quit
    const auto t_now = std::chrono::high_resolution_clock::now();
    const auto t_diff = std::chrono::duration_cast<std::chrono::milliseconds>(t_now - t_last).count();
    if (t_diff < 2000) {
      std::this_thread::sleep_for(std::chrono::milliseconds(100));
      continue;
    }

    audio.get(2000, pcmf32_new);
    if (!::vad_simple(pcmf32_new, WHISPER_SAMPLE_RATE, 1000, vad_thold, freq_thold, false)) {
      std::this_thread::sleep_for(std::chrono::milliseconds(100));
      continue;
    }
    {
      long long span = t_diff;              // ms since the last transcription
      if (span > kBufferMs) span = kBufferMs;
      audio.get((int) span, pcmf32);
    }
    t_last = t_now;

    whisper_full_params wparams = whisper_full_default_params(WHISPER_SAMPLING_GREEDY);
    wparams.print_progress = false;
    wparams.print_special = false;
    wparams.print_realtime = false;
    wparams.print_timestamps = false;
    wparams.translate = false;
    wparams.single_segment = false;
    wparams.max_tokens = 0;
    wparams.language = lang.c_str();
    wparams.n_threads = 4;
    wparams.no_context = true;
    wparams.progress_callback = on_progress;

    TranscribingScope busy;
    if (whisper_full(ctx, wparams, pcmf32.data(), pcmf32.size()) != 0) {
      emit("{\"type\":\"canceled\",\"error\":\"inference failed\"}");
      break;
    }

    std::string text;
    const int n = whisper_full_n_segments(ctx);
    for (int i = 0; i < n; ++i) text += whisper_full_get_segment_text(ctx, i);
    text = trim(text);
    // Skip empties + whisper's silence hallucination markers.
    if (!is_noise_marker(text)) {
      emit("{\"type\":\"final\",\"text\":\"" + json_escape(text) + "\",\"language\":\"" + json_escape(locale) + "\"}");
    }
  }

  // FLUSH THE TAIL. The loop only transcribes when the VAD sees a pause, so the
  // audio spoken between the last segment and the stop is still buffered. Exiting
  // here would discard it — which silently truncated the end of every dictation,
  // and was most visible on long ones (a 12.6 s tail was lost in the report that
  // led to this fix).
  {
    const auto t_end = std::chrono::high_resolution_clock::now();
    long long tail_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(t_end - t_last).count();
    // Cap at the window we transcribe anyway, and ignore a sliver that can only
    // be the release itself. Asking for exactly the untranscribed span (not the
    // full window) is what keeps this from repeating words already emitted.
    if (tail_ms > 400) {
      if (tail_ms > kBufferMs) tail_ms = kBufferMs;
      audio.get((int) tail_ms, pcmf32);
      // Only transcribe a tail that actually contains sound. whisper hallucinates
      // confident sentences out of silence — observed: a silent 6 s tail produced
      // "a un reino entero solo por aburrimiento" — and the flush would paste that
      // straight into the user's document. A peak-amplitude floor is enough to
      // tell speech from a quiet room, and unlike VAD it does not require the
      // trailing silence that this very fix exists to handle.
      float peak = 0.0f;
      for (float v : pcmf32) { const float a = v < 0 ? -v : v; if (a > peak) peak = a; }
      const bool has_sound = peak > 0.03f; // measured: a silent room peaks ~0.02

      if (has_sound && !pcmf32.empty()) {
        whisper_full_params wparams = whisper_full_default_params(WHISPER_SAMPLING_GREEDY);
        wparams.print_progress = false;
        wparams.print_special = false;
        wparams.print_realtime = false;
        wparams.print_timestamps = false;
        wparams.translate = false;
        wparams.single_segment = false;
        wparams.max_tokens = 0;
        wparams.language = lang.c_str();
        wparams.n_threads = 4;
        wparams.no_context = true;
        // The flush is where this matters most: the parent is already waiting to
        // stop, and without a heartbeat it kills the helper mid-pass.
        wparams.progress_callback = on_progress;
        TranscribingScope busy;
        if (whisper_full(ctx, wparams, pcmf32.data(), pcmf32.size()) == 0) {
          std::string text;
          const int n = whisper_full_n_segments(ctx);
          for (int i = 0; i < n; ++i) text += whisper_full_get_segment_text(ctx, i);
          text = trim(text);
          if (!is_noise_marker(text)) {
            emit("{\"type\":\"final\",\"text\":\"" + json_escape(text) +
                 "\",\"language\":\"" + json_escape(locale) + "\"}");
          }
        }
      }
    }
  }

  audio.pause();
  whisper_free(ctx);
  emit("{\"type\":\"stopped\"}");
  return 0;
}
