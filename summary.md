# Bug Fix Summary

## Bug 1 — PCM_MULAW (G.711 µ-law) Audio Not Recorded

### Root Cause A — Wrong stream index for AAC packets
**File:** `machinery/src/capture/gortsplib.go`

When forwarding decoded audio packets to the queue, AAC packets were tagged with `g.AudioG711Index` instead of `g.AudioMPEG4Index`. This caused audio samples to be associated with the wrong stream, breaking playback.

**Fix:** Changed the packet index to `g.AudioMPEG4Index` so AAC packets correctly reference the MPEG-4 audio stream.

---

### Root Cause B — PCM_MULAW codec had no recording path
**File:** `machinery/src/capture/main.go`, new file `machinery/src/capture/mulaw_to_aac.go`

Cameras that stream G.711 µ-law audio (PCM_MULAW) had no transcoding path to MP4. Raw µ-law bytes cannot be stored directly in fragmented MP4, so audio was silently dropped.

**Fix:** Added a new FFmpeg CGo transcoder (`MulawToAACEncoder`) in `mulaw_to_aac.go` that:
- Decodes raw µ-law bytes to S16 PCM via `github.com/zaf/g711`
- Resamples S16 → FLTP using `libswresample`
- Encodes to AAC-LC at 32 kbps using `libavcodec`
- Wraps each output frame in a 7-byte ADTS header

The encoder is created per recording segment in `HandleRecordStream()` for both continuous and motion-detection recording modes, and feeds directly into the existing `SplitAACFrame` / `mp4ff` AAC pipeline. The audio track is registered as `"AAC"` so `mp4.Close()` handles it correctly without any changes to `video/mp4.go`.

---

## Bug 2 — H265 (HEVC) Cameras Fail to Connect and Record

### Root Cause A — SDP without SPS caused Connect() to abort
**File:** `machinery/src/capture/gortsplib.go` — `Connect()` function

Many H265 cameras do not include SPS (Sequence Parameter Set) in the SDP offer; instead they send VPS/SPS/PPS in-band with the first keyframe. The original code assigned the SPS unmarshal error to the function's named return value `err`, causing `Connect()` to return a non-nil error even though the missing SPS is normal and expected. This caused `kerberos.go` to abort with "error connecting to RTSP stream".

**Fix:** Changed to a local `spsErr` variable (`if spsErr := sps.Unmarshal(...); spsErr != nil`) so the function's named return `err` stays clean. Width, height, and FPS default to zero when SPS is absent from SDP and are updated later from in-band NALUs.

---

### Root Cause B — In-band VPS/SPS/PPS NALUs silently dropped
**File:** `machinery/src/capture/gortsplib.go` — `Start()` function

The H265 packet handler in `Start()` contained placeholder `continue` cases for `VPS_NUT`, `SPS_NUT`, and `PPS_NUT` NALU types, discarding them without processing. This meant the agent never learned the stream's actual resolution, frame rate, or parameter sets, resulting in broken MP4 headers and failed recordings.

**Fix:** The three NALU type cases now:
- Store the NALU back into `g.VideoH265Forma` and `g.Streams[g.VideoH265Index]`
- Parse the in-band SPS to extract width, height, and FPS and write them into `configuration.Config.Capture.IPCamera`
- Populate `SPSNALUs`, `PPSNALUs`, and `VPSNALUs` in config so downstream MP4 muxing has correct parameter sets

---

## Bug 3 — FFmpeg Channel Layout API Incompatibility (FFmpeg 5.1+)

**File:** `machinery/src/capture/mulaw_to_aac.go`

The `MulawToAACEncoder` C code used the legacy `channels` and `channel_layout` fields on `AVCodecContext` and `AVFrame`, and `swr_alloc_set_opts()` with integer channel layout values. These were removed in FFmpeg 5.1 (libavutil 57+), causing compile errors:

```
error: 'AVCodecContext' has no member named 'channels'
error: 'AVCodecContext' has no member named 'channel_layout'; did you mean 'ch_layout'?
error: 'AVFrame' has no member named 'channel_layout'; did you mean 'ch_layout'?
```

**Fix:** Migrated to the FFmpeg 5.1+ `AVChannelLayout` API:
- `av_channel_layout_default(&ch_layout, channels)` to initialize the layout
- `av_channel_layout_copy(&ctx->ch_layout, &ch_layout)` to assign it to encoder and frame contexts
- `swr_alloc_set_opts2(&swr_ctx, &ch_layout, ...)` replaces the deprecated `swr_alloc_set_opts()`
- `av_channel_layout_uninit(&ch_layout)` added on all exit paths to free layout resources

---

## Feature — Runtime Audio Enable/Disable Flag

**Files:** `machinery/src/models/config.go`, `machinery/src/capture/main.go`

Added an `Audio` field to the `Capture` config struct (same `string` pattern as `Recording`, `Liveview`, `Motion`, etc.) so audio capture can be turned off without rebuilding the binary.

**Usage:** In `config.json`, set `"audio": "false"` under the `capture` section to disable all audio recording. Omitting the field or setting it to `"true"` preserves the existing behavior.

```json
"capture": {
  "recording": "true",
  "continuous": "true",
  "motion": "false",
  "audio": "false"
}
```

When disabled, `audioCodec` stays empty and no audio track is created or written in either the continuous or motion-detection recording path.
