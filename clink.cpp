#include "clink.h"
#include <ableton/LinkAudio.hpp>
#include <vector>
#include <string>
#include <chrono>
#include <cstring>

extern "C"
{
  clink clink_create(double bpm, const char* name)
  {
    const char* peer_name = (name && strlen(name) > 0) ? name : "icecast";
    return clink{reinterpret_cast<void *>(new ableton::LinkAudio(bpm, peer_name))};
  }

  void clink_destroy(clink link)
  {
    delete reinterpret_cast<ableton::LinkAudio *>(link.impl);
  }

  bool clink_is_enabled(clink link)
  {
    return reinterpret_cast<ableton::LinkAudio *>(link.impl)->isEnabled();
  }

  void clink_enable(clink link, bool enabled)
  {
    reinterpret_cast<ableton::LinkAudio *>(link.impl)->enable(enabled);
  }

  bool clink_is_audio_enabled(clink link)
  {
    return reinterpret_cast<ableton::LinkAudio *>(link.impl)->isLinkAudioEnabled();
  }

  void clink_enable_audio(clink link, bool enabled)
  {
    reinterpret_cast<ableton::LinkAudio *>(link.impl)->enableLinkAudio(enabled);
  }

  void clink_set_peer_name(clink link, const char* name)
  {
    reinterpret_cast<ableton::LinkAudio *>(link.impl)->setPeerName(name ? name : "");
  }

  bool clink_is_start_stop_sync_enabled(clink link)
  {
    return reinterpret_cast<ableton::LinkAudio *>(link.impl)->isStartStopSyncEnabled();
  }

  void clink_enable_start_stop_sync(clink link, bool enabled)
  {
    reinterpret_cast<ableton::LinkAudio *>(link.impl)->enableStartStopSync(enabled);
  }

  uint64_t clink_num_peers(clink link)
  {
    return reinterpret_cast<ableton::LinkAudio *>(link.impl)->numPeers();
  }

  void clink_set_num_peers_callback(clink link, clink_num_peers_callback callback, void *context)
  {
    reinterpret_cast<ableton::LinkAudio *>(link.impl)->setNumPeersCallback(
      [callback, context](std::size_t numPeers)
      { (*callback)(static_cast<uint64_t>(numPeers), context); });
  }

  void clink_set_tempo_callback(clink link, clink_tempo_callback callback, void *context)
  {
    reinterpret_cast<ableton::LinkAudio *>(link.impl)->setTempoCallback(
      [callback, context](double tempo) { (*callback)(tempo, context); });
  }

  void clink_set_start_stop_callback(clink link, clink_start_stop_callback callback, void *context)
  {
    reinterpret_cast<ableton::LinkAudio *>(link.impl)->setStartStopCallback(
      [callback, context](bool isPlaying) { (*callback)(isPlaying, context); });
  }

  void clink_set_channels_changed_callback(clink link, clink_channels_changed_callback callback, void *context)
  {
    reinterpret_cast<ableton::LinkAudio *>(link.impl)->setChannelsChangedCallback(
      [callback, context]() { (*callback)(context); });
  }

  int64_t clink_clock_micros(clink link)
  {
    return reinterpret_cast<ableton::LinkAudio *>(link.impl)->clock().micros().count();
  }

  clink_session_state clink_create_session_state(void)
  {
    return clink_session_state{reinterpret_cast<void *>(
      new ableton::LinkAudio::SessionState{ableton::link::ApiState{}, {}})};
  }

  void clink_destroy_session_state(clink_session_state session_state)
  {
    delete reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl);
  }

  void clink_capture_app_session_state(clink link, clink_session_state session_state)
  {
    *reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl) =
      reinterpret_cast<ableton::LinkAudio *>(link.impl)->captureAppSessionState();
  }

  void clink_commit_app_session_state(clink link, clink_session_state session_state)
  {
    reinterpret_cast<ableton::LinkAudio *>(link.impl)->commitAppSessionState(
      *reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl));
  }

  void clink_capture_audio_session_state(clink link, clink_session_state session_state)
  {
    *reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl) =
      reinterpret_cast<ableton::LinkAudio *>(link.impl)->captureAudioSessionState();
  }

  void clink_commit_audio_session_state(clink link, clink_session_state session_state)
  {
    reinterpret_cast<ableton::LinkAudio *>(link.impl)->commitAudioSessionState(
      *reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl));
  }

  double clink_tempo(clink_session_state session_state)
  {
    return reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl)->tempo();
  }

  void clink_set_tempo(clink_session_state session_state, double bpm, int64_t at_time)
  {
    reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl)
      ->setTempo(bpm, std::chrono::microseconds{at_time});
  }

  double clink_beat_at_time(clink_session_state session_state, int64_t time, double quantum)
  {
    return reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl)
      ->beatAtTime(std::chrono::microseconds{time}, quantum);
  }

  double clink_phase_at_time(clink_session_state session_state, int64_t time, double quantum)
  {
    return reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl)
      ->phaseAtTime(std::chrono::microseconds{time}, quantum);
  }

  int64_t clink_time_at_beat(clink_session_state session_state, double beat, double quantum)
  {
    return reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl)
      ->timeAtBeat(beat, quantum).count();
  }

  void clink_request_beat_at_time(clink_session_state session_state, double beat, int64_t time, double quantum)
  {
    reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl)
      ->requestBeatAtTime(beat, std::chrono::microseconds{time}, quantum);
  }

  void clink_force_beat_at_time(clink_session_state session_state, double beat, int64_t time, double quantum)
  {
    reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl)
      ->forceBeatAtTime(beat, std::chrono::microseconds{time}, quantum);
  }

  void clink_set_is_playing(clink_session_state session_state, bool is_playing, int64_t time)
  {
    reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl)
      ->setIsPlaying(is_playing, std::chrono::microseconds(time));
  }

  bool clink_is_playing(clink_session_state session_state)
  {
    return reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl)->isPlaying();
  }

  int64_t clink_time_for_is_playing(clink_session_state session_state)
  {
    return static_cast<int64_t>(reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl)
        ->timeForIsPlaying().count());
  }

  void clink_request_beat_at_start_playing_time(clink_session_state session_state, double beat, double quantum)
  {
    reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl)
      ->requestBeatAtStartPlayingTime(beat, quantum);
  }

  void clink_set_is_playing_and_request_beat_at_time(clink_session_state session_state, bool is_playing, int64_t time, double beat, double quantum)
  {
    reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl)
      ->setIsPlayingAndRequestBeatAtTime(is_playing, std::chrono::microseconds{time}, beat, quantum);
  }

  clink_channels clink_capture_channels(clink link)
  {
    return clink_channels{reinterpret_cast<void *>(
      new std::vector<ableton::LinkAudio::Channel>(
        reinterpret_cast<ableton::LinkAudio *>(link.impl)->channels()))};
  }

  void clink_destroy_channels(clink_channels channels)
  {
    delete reinterpret_cast<std::vector<ableton::LinkAudio::Channel> *>(channels.impl);
  }

  uint64_t clink_channels_count(clink_channels channels)
  {
    return static_cast<uint64_t>(
      reinterpret_cast<std::vector<ableton::LinkAudio::Channel> *>(channels.impl)->size());
  }

  uint64_t clink_channel_id(clink_channels channels, uint64_t index)
  {
    const auto& c = (*reinterpret_cast<std::vector<ableton::LinkAudio::Channel> *>(channels.impl))[index];
    uint64_t id = 0;
    std::memcpy(&id, c.id.data(), sizeof(uint64_t));
    return id;
  }

  const char* clink_channel_name(clink_channels channels, uint64_t index)
  {
    return (*reinterpret_cast<std::vector<ableton::LinkAudio::Channel> *>(channels.impl))[index].name.c_str();
  }

  uint64_t clink_channel_peer_id(clink_channels channels, uint64_t index)
  {
    const auto& c = (*reinterpret_cast<std::vector<ableton::LinkAudio::Channel> *>(channels.impl))[index];
    uint64_t id = 0;
    std::memcpy(&id, c.peerId.data(), sizeof(uint64_t));
    return id;
  }

  const char* clink_channel_peer_name(clink_channels channels, uint64_t index)
  {
    return (*reinterpret_cast<std::vector<ableton::LinkAudio::Channel> *>(channels.impl))[index].peerName.c_str();
  }

  clink_sink clink_create_sink(clink link, const char* name, uint64_t max_num_samples)
  {
    return clink_sink{reinterpret_cast<void *>(
      new ableton::LinkAudioSink(*reinterpret_cast<ableton::LinkAudio *>(link.impl), name, max_num_samples))};
  }

  void clink_destroy_sink(clink_sink sink)
  {
    delete reinterpret_cast<ableton::LinkAudioSink *>(sink.impl);
  }

  const char* clink_sink_name(clink_sink sink)
  {
    static thread_local std::string name;
    name = reinterpret_cast<ableton::LinkAudioSink *>(sink.impl)->name();
    return name.c_str();
  }

  void clink_sink_set_name(clink_sink sink, const char* name)
  {
    reinterpret_cast<ableton::LinkAudioSink *>(sink.impl)->setName(name);
  }

  void clink_sink_request_max_num_samples(clink_sink sink, uint64_t num_samples)
  {
    reinterpret_cast<ableton::LinkAudioSink *>(sink.impl)->requestMaxNumSamples(num_samples);
  }

  uint64_t clink_sink_max_num_samples(clink_sink sink)
  {
    return reinterpret_cast<ableton::LinkAudioSink *>(sink.impl)->maxNumSamples();
  }

  bool clink_sink_retain_buffer(clink_sink sink, clink_sink_buffer* buffer)
  {
    auto handle = new ableton::LinkAudioSink::BufferHandle(*reinterpret_cast<ableton::LinkAudioSink *>(sink.impl));
    if (*handle)
    {
      buffer->samples = handle->samples;
      buffer->max_num_samples = handle->maxNumSamples;
      buffer->handle = reinterpret_cast<void *>(handle);
      return true;
    }
    delete handle;
    return false;
  }

  bool clink_sink_commit_buffer(clink_sink sink, clink_sink_buffer* buffer, clink_session_state session_state, double beats_at_buffer_begin, double quantum, uint64_t num_frames, uint64_t num_channels, uint32_t sample_rate)
  {
    (void)sink;
    auto handle = reinterpret_cast<ableton::LinkAudioSink::BufferHandle *>(buffer->handle);
    bool result = handle->commit(
      *reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl),
      beats_at_buffer_begin,
      quantum,
      num_frames,
      num_channels,
      sample_rate);
    delete handle;
    buffer->handle = nullptr;
    buffer->samples = nullptr;
    return result;
  }

  void clink_sink_release_buffer(clink_sink sink, clink_sink_buffer* buffer)
  {
    (void)sink;
    if (buffer->handle)
    {
      delete reinterpret_cast<ableton::LinkAudioSink::BufferHandle *>(buffer->handle);
      buffer->handle = nullptr;
      buffer->samples = nullptr;
    }
  }

  clink_source clink_create_source(clink link, uint64_t channel_id, clink_source_callback callback, void* context)
  {
    ableton::ChannelId id;
    std::memcpy(id.data(), &channel_id, sizeof(uint64_t));

    return clink_source{reinterpret_cast<void *>(
      new ableton::LinkAudioSource(
        *reinterpret_cast<ableton::LinkAudio *>(link.impl),
        id,
        [callback, context](ableton::LinkAudioSource::BufferHandle handle) {
          clink_source_buffer_info info;
          info.num_channels = handle.info.numChannels;
          info.num_frames = handle.info.numFrames;
          info.sample_rate = handle.info.sampleRate;
          info.count = handle.info.count;
          info.session_beat_time = handle.info.sessionBeatTime;
          std::memcpy(&info.session_id, handle.info.sessionId.data(), sizeof(uint64_t));
          info.tempo = handle.info.tempo;

          callback(handle.samples, &info, context);
        }))};
  }

  void clink_destroy_source(clink_source source)
  {
    delete reinterpret_cast<ableton::LinkAudioSource *>(source.impl);
  }

  uint64_t clink_source_channel_id(clink_source source)
  {
    auto id = reinterpret_cast<ableton::LinkAudioSource *>(source.impl)->id();
    uint64_t cid = 0;
    std::memcpy(&cid, id.data(), sizeof(uint64_t));
    return cid;
  }

  bool clink_source_info_begin_beats(clink_source_buffer_info* info, clink_session_state session_state, double quantum, double* out_beats)
  {
    ableton::LinkAudioSource::BufferHandle::Info cpp_info;
    cpp_info.numChannels = info->num_channels;
    cpp_info.numFrames = info->num_frames;
    cpp_info.sampleRate = info->sample_rate;
    cpp_info.count = info->count;
    cpp_info.sessionBeatTime = info->session_beat_time;
    cpp_info.tempo = info->tempo;
    std::memcpy(cpp_info.sessionId.data(), &info->session_id, sizeof(uint64_t));

    auto beats = cpp_info.beginBeats(*reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl), quantum);
    if (beats)
    {
      *out_beats = *beats;
      return true;
    }
    return false;
  }

  bool clink_source_info_end_beats(clink_source_buffer_info* info, clink_session_state session_state, double quantum, double* out_beats)
  {
    ableton::LinkAudioSource::BufferHandle::Info cpp_info;
    cpp_info.numChannels = info->num_channels;
    cpp_info.numFrames = info->num_frames;
    cpp_info.sampleRate = info->sample_rate;
    cpp_info.count = info->count;
    cpp_info.sessionBeatTime = info->session_beat_time;
    cpp_info.tempo = info->tempo;
    std::memcpy(cpp_info.sessionId.data(), &info->session_id, sizeof(uint64_t));

    auto beats = cpp_info.endBeats(*reinterpret_cast<ableton::LinkAudio::SessionState *>(session_state.impl), quantum);
    if (beats)
    {
      *out_beats = *beats;
      return true;
    }
    return false;
  }
}
