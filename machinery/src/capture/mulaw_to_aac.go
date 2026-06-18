package capture

/*
#cgo pkg-config: libavcodec libavutil libswresample
#cgo CFLAGS: -Wno-deprecated-declarations
#include <libavcodec/avcodec.h>
#include <libavutil/channel_layout.h>
#include <libavutil/frame.h>
#include <libavutil/mem.h>
#include <libavutil/opt.h>
#include <libswresample/swresample.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
    AVCodecContext *enc_ctx;
    SwrContext     *swr_ctx;
    AVFrame        *frame;
    AVPacket       *pkt;
    int             frame_size;
} mulaw_aac_enc_t;

static mulaw_aac_enc_t* mulaw_aac_enc_create(int sample_rate, int channels) {
    const AVCodec *codec = avcodec_find_encoder(AV_CODEC_ID_AAC);
    if (!codec) return NULL;

    mulaw_aac_enc_t *e = (mulaw_aac_enc_t*)calloc(1, sizeof(mulaw_aac_enc_t));
    if (!e) return NULL;

    e->enc_ctx = avcodec_alloc_context3(codec);
    if (!e->enc_ctx) { free(e); return NULL; }

    AVChannelLayout ch_layout;
    av_channel_layout_default(&ch_layout, channels);

    e->enc_ctx->sample_rate = sample_rate;
    e->enc_ctx->sample_fmt  = AV_SAMPLE_FMT_FLTP;
    e->enc_ctx->bit_rate    = 32000;
    av_channel_layout_copy(&e->enc_ctx->ch_layout, &ch_layout);

    if (avcodec_open2(e->enc_ctx, codec, NULL) < 0) {
        avcodec_free_context(&e->enc_ctx);
        av_channel_layout_uninit(&ch_layout);
        free(e);
        return NULL;
    }

    e->frame_size = e->enc_ctx->frame_size;

    e->frame = av_frame_alloc();
    if (!e->frame) {
        avcodec_free_context(&e->enc_ctx);
        av_channel_layout_uninit(&ch_layout);
        free(e);
        return NULL;
    }
    e->frame->nb_samples = e->frame_size;
    e->frame->format     = AV_SAMPLE_FMT_FLTP;
    av_channel_layout_copy(&e->frame->ch_layout, &ch_layout);
    if (av_frame_get_buffer(e->frame, 0) < 0) {
        av_frame_free(&e->frame);
        avcodec_free_context(&e->enc_ctx);
        av_channel_layout_uninit(&ch_layout);
        free(e);
        return NULL;
    }

    e->pkt = av_packet_alloc();
    if (!e->pkt) {
        av_frame_free(&e->frame);
        avcodec_free_context(&e->enc_ctx);
        av_channel_layout_uninit(&ch_layout);
        free(e);
        return NULL;
    }

    if (swr_alloc_set_opts2(&e->swr_ctx,
            &ch_layout, AV_SAMPLE_FMT_FLTP, sample_rate,
            &ch_layout, AV_SAMPLE_FMT_S16,  sample_rate,
            0, NULL) < 0 || swr_init(e->swr_ctx) < 0) {
        if (e->swr_ctx) swr_free(&e->swr_ctx);
        av_packet_free(&e->pkt);
        av_frame_free(&e->frame);
        avcodec_free_context(&e->enc_ctx);
        av_channel_layout_uninit(&ch_layout);
        free(e);
        return NULL;
    }

    av_channel_layout_uninit(&ch_layout);
    return e;
}

static void mulaw_aac_enc_destroy(mulaw_aac_enc_t *e) {
    if (!e) return;
    if (e->swr_ctx)  swr_free(&e->swr_ctx);
    if (e->frame)    av_frame_free(&e->frame);
    if (e->pkt)      av_packet_free(&e->pkt);
    if (e->enc_ctx)  avcodec_free_context(&e->enc_ctx);
    free(e);
}

static int mulaw_aac_enc_encode_frame(mulaw_aac_enc_t *e,
                                       const int16_t   *s16_samples,
                                       uint8_t        **out_data,
                                       int             *out_size) {
    *out_data = NULL;
    *out_size = 0;

    const uint8_t *in_data[1] = { (const uint8_t*)s16_samples };
    if (av_frame_make_writable(e->frame) < 0) return -1;
    int n = swr_convert(e->swr_ctx,
                        e->frame->data, e->frame_size,
                        in_data,        e->frame_size);
    if (n < 0) return -1;

    if (avcodec_send_frame(e->enc_ctx, e->frame) < 0) return -1;

    int    buf_cap = 4096;
    uint8_t *buf  = (uint8_t*)av_malloc(buf_cap);
    if (!buf) return -1;
    int buf_len = 0;

    while (avcodec_receive_packet(e->enc_ctx, e->pkt) == 0) {
        int needed = buf_len + e->pkt->size;
        if (needed > buf_cap) {
            buf_cap = needed * 2;
            uint8_t *tmp = (uint8_t*)av_realloc(buf, buf_cap);
            if (!tmp) { av_packet_unref(e->pkt); av_free(buf); return -1; }
            buf = tmp;
        }
        memcpy(buf + buf_len, e->pkt->data, e->pkt->size);
        buf_len += e->pkt->size;
        av_packet_unref(e->pkt);
    }

    if (buf_len == 0) { av_free(buf); return 0; }
    *out_data = buf;
    *out_size = buf_len;
    return 0;
}
*/
import "C"

import (
	"encoding/binary"
	"errors"
	"unsafe"

	"github.com/kerberos-io/agent/machinery/src/log"
	"github.com/zaf/g711"
)

// MulawToAACEncoder transcodes raw G.711 µ-law bytes (8 kHz mono) to
// ADTS-wrapped AAC-LC frames for the existing SplitAACFrame / mp4ff pipeline.
type MulawToAACEncoder struct {
	handle     *C.mulaw_aac_enc_t
	frameSize  int
	sampleRate int
	channels   int
	s16Buf     []int16
}

// NewMulawToAACEncoder creates an encoder for 8 kHz mono G.711 µ-law input.
func NewMulawToAACEncoder() (*MulawToAACEncoder, error) {
	const sampleRate = 8000
	const channels = 1
	h := C.mulaw_aac_enc_create(C.int(sampleRate), C.int(channels))
	if h == nil {
		return nil, errors.New("capture.MulawToAACEncoder: FFmpeg AAC encoder unavailable")
	}
	log.Log.Info("capture.MulawToAACEncoder: initialised (8 kHz mono PCM_MULAW → AAC-LC @ 32 kbps)")
	return &MulawToAACEncoder{
		handle:     h,
		frameSize:  int(h.frame_size),
		sampleRate: sampleRate,
		channels:   channels,
	}, nil
}

// SampleRate returns the encoder's sample rate (8000).
func (e *MulawToAACEncoder) SampleRate() int { return e.sampleRate }

// Encode decodes µ-law bytes to S16 PCM, buffers them, and returns zero or
// more ADTS-wrapped AAC frames once complete 1024-sample frames are available.
func (e *MulawToAACEncoder) Encode(mulawData []byte) ([]byte, error) {
	if e == nil || e.handle == nil || len(mulawData) == 0 {
		return nil, nil
	}

	pcmBytes := g711.DecodeUlaw(mulawData)
	nSamples := len(pcmBytes) / 2
	for i := 0; i < nSamples; i++ {
		sample := int16(binary.LittleEndian.Uint16(pcmBytes[i*2 : i*2+2]))
		e.s16Buf = append(e.s16Buf, sample)
	}

	var adtsOut []byte
	for len(e.s16Buf) >= e.frameSize {
		frame := e.s16Buf[:e.frameSize]
		e.s16Buf = e.s16Buf[e.frameSize:]

		var outData *C.uint8_t
		var outSize C.int
		ret := C.mulaw_aac_enc_encode_frame(
			e.handle,
			(*C.int16_t)(unsafe.Pointer(&frame[0])),
			&outData, &outSize,
		)
		if ret < 0 || outSize == 0 || outData == nil {
			continue
		}
		rawAAC := C.GoBytes(unsafe.Pointer(outData), outSize)
		C.av_free(unsafe.Pointer(outData))

		adts := mulawBuildADTSHeader(e.sampleRate, e.channels, len(rawAAC))
		adtsOut = append(adtsOut, adts...)
		adtsOut = append(adtsOut, rawAAC...)
	}
	return adtsOut, nil
}

// Close releases all FFmpeg resources.
func (e *MulawToAACEncoder) Close() {
	if e != nil && e.handle != nil {
		C.mulaw_aac_enc_destroy(e.handle)
		e.handle = nil
		log.Log.Info("capture.MulawToAACEncoder: closed")
	}
}

func mulawBuildADTSHeader(sampleRate, channels, rawAACLen int) []byte {
	freqIdx := mulawADTSSampleRateIndex(sampleRate)
	frameLen := 7 + rawAACLen
	hdr := make([]byte, 7)
	hdr[0] = 0xFF
	hdr[1] = 0xF1
	hdr[2] = byte(1<<6) | byte(freqIdx<<2) | byte((channels>>2)&1)
	hdr[3] = byte((channels&0x3)<<6) | byte((frameLen>>11)&0x3)
	hdr[4] = byte((frameLen >> 3) & 0xFF)
	hdr[5] = byte((frameLen&0x7)<<5) | 0x1F
	hdr[6] = 0xFC
	return hdr
}

func mulawADTSSampleRateIndex(sampleRate int) int {
	rates := [13]int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}
	for i, r := range rates {
		if r == sampleRate {
			return i
		}
	}
	return 11
}
