class MixerWorklet extends AudioWorkletProcessor {
  constructor() {
    super();
    this.channels = new Map(); // id -> { buffer: Float32Array, gain: number, mute: bool, solo: bool }
    this.masterGain = 1.0;
    this.masterMute = false;
    
    this.port.onmessage = (e) => {
      const { type, data } = e.data;
      if (type === 'audio') {
        // data: { id, samples: Float32Array }
        let ch = this.channels.get(data.id);
        if (!ch) {
          ch = { buffer: new Float32Array(0), gain: 0.8, mute: false, solo: false };
          this.channels.set(data.id, ch);
        }
        // Append to local jitter buffer
        const newBuf = new Float32Array(ch.buffer.length + data.samples.length);
        newBuf.set(ch.buffer);
        newBuf.set(data.samples, ch.buffer.length);
        ch.buffer = newBuf;
        
        // Trim if too large (> 1s)
        if (ch.buffer.length > 48000 * 2) {
          ch.buffer = ch.buffer.slice(ch.buffer.length - 48000);
        }
      } else if (type === 'params') {
        // data: { masterVol, masterMute, channels: { id: { vol, mute, solo } } }
        this.masterGain = data.masterVol;
        this.masterMute = data.masterMute;
        for (const id in data.channels) {
          const target = this.channels.get(id);
          if (target) {
            target.gain = data.channels[id].vol;
            target.mute = data.channels[id].mute;
            target.solo = data.channels[id].solo;
          }
        }
      } else if (type === 'remove') {
        this.channels.delete(data.id);
      }
    };
  }

  process(inputs, outputs, parameters) {
    const output = outputs[0];
    const frameCount = output[0].length;
    
    // Clear output
    for (let c = 0; c < output.length; c++) output[c].fill(0);

    const activeChannels = Array.from(this.channels.values());
    const anySolo = activeChannels.some(ch => ch.solo);

    for (const [id, ch] of this.channels) {
      if (ch.buffer.length < frameCount * 2) continue; // Underrun

      const samples = ch.buffer.slice(0, frameCount * 2);
      ch.buffer = ch.buffer.slice(frameCount * 2);

      if (ch.mute || (anySolo && !ch.solo)) continue;

      for (let i = 0; i < frameCount; i++) {
        const g = ch.gain * this.masterGain * (this.masterMute ? 0 : 1);
        output[0][i] += samples[i * 2] * g;
        output[1][i] += samples[i * 2 + 1] * g;
      }
    }

    return true;
  }
}

registerProcessor('mixer-worklet', MixerWorklet);
