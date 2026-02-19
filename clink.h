#ifndef CLINK_H
#define CLINK_H

#include <stdbool.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C"
{
#endif

  typedef struct clink
  {
    void *impl;
  } clink;

  typedef struct clink_session_state
  {
    void *impl;
  } clink_session_state;

  typedef struct clink_channels
  {
    void* impl;
  } clink_channels;

  typedef struct clink_sink
  {
    void* impl;
  } clink_sink;

  typedef struct clink_sink_buffer
  {
    int16_t* samples;
    uint64_t max_num_samples;
    void* handle;
  } clink_sink_buffer;

  typedef struct clink_source
  {
    void* impl;
  } clink_source;

  typedef struct clink_source_buffer_info
  {
    uint64_t num_channels;
    uint64_t num_frames;
    uint32_t sample_rate;
    uint64_t count;
    double session_beat_time;
    uint64_t session_id;
    double tempo;
  } clink_source_buffer_info;

  // Link instance
  clink clink_create(double bpm, const char* name);
  void clink_destroy(clink link);
  bool clink_is_enabled(clink link);
  void clink_enable(clink link, bool enable);
  bool clink_is_audio_enabled(clink link);
  void clink_enable_audio(clink link, bool enable);
  void clink_set_peer_name(clink link, const char* name);
  bool clink_is_start_stop_sync_enabled(clink link);
  void clink_enable_start_stop_sync(clink link, bool enabled);
  uint64_t clink_num_peers(clink link);
  int64_t clink_clock_micros(clink link);

  // Callbacks
  typedef void (*clink_num_peers_callback)(uint64_t num_peers, void *context);
  void clink_set_num_peers_callback(clink link, clink_num_peers_callback callback, void *context);

  typedef void (*clink_tempo_callback)(double tempo, void *context);
  void clink_set_tempo_callback(clink link, clink_tempo_callback callback, void *context);

  typedef void (*clink_start_stop_callback)(bool is_playing, void *context);
  void clink_set_start_stop_callback(clink link, clink_start_stop_callback callback, void *context);

  typedef void (*clink_channels_changed_callback)(void *context);
  void clink_set_channels_changed_callback(clink link, clink_channels_changed_callback callback, void *context);

  // Session State
  clink_session_state clink_create_session_state(void);
  void clink_destroy_session_state(clink_session_state session_state);
  void clink_capture_app_session_state(clink link, clink_session_state session_state);
  void clink_commit_app_session_state(clink link, clink_session_state session_state);
  void clink_capture_audio_session_state(clink link, clink_session_state session_state);
  void clink_commit_audio_session_state(clink link, clink_session_state session_state);

  double clink_tempo(clink_session_state session_state);
  void clink_set_tempo(clink_session_state session_state, double bpm, int64_t at_time);
  double clink_beat_at_time(clink_session_state session_state, int64_t time, double quantum);
  double clink_phase_at_time(clink_session_state session_state, int64_t time, double quantum);
  int64_t clink_time_at_beat(clink_session_state session_state, double beat, double quantum);
  void clink_request_beat_at_time(clink_session_state session_state, double beat, int64_t time, double quantum);
  void clink_force_beat_at_time(clink_session_state session_state, double beat, int64_t time, double quantum);
  void clink_set_is_playing(clink_session_state session_state, bool is_playing, int64_t time);
  bool clink_is_playing(clink_session_state session_state);
  int64_t clink_time_for_is_playing(clink_session_state session_state);
  void clink_request_beat_at_start_playing_time(clink_session_state session_state, double beat, double quantum);
  void clink_set_is_playing_and_request_beat_at_time(clink_session_state session_state, bool is_playing, int64_t time, double beat, double quantum);

  // Channels
  clink_channels clink_capture_channels(clink link);
  void clink_destroy_channels(clink_channels channels);
  uint64_t clink_channels_count(clink_channels channels);
  uint64_t clink_channel_id(clink_channels channels, uint64_t index);
  const char* clink_channel_name(clink_channels channels, uint64_t index);
  uint64_t clink_channel_peer_id(clink_channels channels, uint64_t index);
  const char* clink_channel_peer_name(clink_channels channels, uint64_t index);

  // Sink
  clink_sink clink_create_sink(clink link, const char* name, uint64_t max_num_samples);
  void clink_destroy_sink(clink_sink sink);
  const char* clink_sink_name(clink_sink sink);
  void clink_sink_set_name(clink_sink sink, const char* name);
  void clink_sink_request_max_num_samples(clink_sink sink, uint64_t num_samples);
  uint64_t clink_sink_max_num_samples(clink_sink sink);
  bool clink_sink_retain_buffer(clink_sink sink, clink_sink_buffer* buffer);
  bool clink_sink_commit_buffer(clink_sink sink, clink_sink_buffer* buffer, clink_session_state session_state, double beats_at_buffer_begin, double quantum, uint64_t num_frames, uint64_t num_channels, uint32_t sample_rate);
  void clink_sink_release_buffer(clink_sink sink, clink_sink_buffer* buffer);

  // Source
  typedef void (*clink_source_callback)(int16_t* samples, clink_source_buffer_info* info, void* context);
  clink_source clink_create_source(clink link, uint64_t channel_id, clink_source_callback callback, void* context);
  void clink_destroy_source(clink_source source);
  uint64_t clink_source_channel_id(clink_source source);
  bool clink_source_info_begin_beats(clink_source_buffer_info* info, clink_session_state session_state, double quantum, double* out_beats);
  bool clink_source_info_end_beats(clink_source_buffer_info* info, clink_session_state session_state, double quantum, double* out_beats);

#ifdef __cplusplus
}
#endif

#endif
