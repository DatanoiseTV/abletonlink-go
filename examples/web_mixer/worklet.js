class MixerWorklet extends AudioWorkletProcessor {
  constructor() {
    super();
    this.channels = new Map();
    this.masterGain = 1.0;
    this.masterMute = false;
    
    this.port.onmessage = (e) => {
      const { type, data } = e.data;
      if (type === 'audio') {
        let ch = this.channels.get(data.id);
        if (!ch) {
          ch = { buffer: new Float32Array(0), gain: 0.8, mute: false, solo: false };
          this.channels.set(data.id, ch);
        }
        
        // Convert Int16 to Float32 during append
        const f32 = new Float32Array(data.samples.length);
        for(let i=0; i<data.samples.length; i++) f32[i] = data.samples[i] / 32768.0;

        const newBuf = new Float32Array(ch.buffer.length + f32.length);
        newBuf.set(ch.buffer);
        newBuf.set(f32, ch.buffer.length);
        ch.buffer = newBuf;
        
        if (ch.buffer.length > 48000 * 2) ch.buffer = ch.buffer.slice(ch.buffer.length - 48000);
      } else if (type === 'params') {
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
    if (!output || output.length === 0) return true;
    
    const frameCount = output[0].length;
    for (let c = 0; c < output.length; c++) output[c].fill(0);

    const activeChannels = Array.from(this.channels.values());
    const anySolo = activeChannels.some(ch => ch.solo);

    for (const [id, ch] of this.channels) {
      if (ch.buffer.length < frameCount * 2) continue;

      const samples = ch.buffer.slice(0, frameCount * 2);
      ch.buffer = ch.buffer.slice(frameCount * 2);

      if (ch.mute || (anySolo && !ch.solo)) continue;

      const g = ch.gain * this.masterGain * (this.masterMute ? 0 : 1);
      const left = output[0];
      const right = output.length > 1 ? output[1] : null;

      for (let i = 0; i < frameCount; i++) {
        left[i] += samples[i * 2] * g;
        if (right) right[i] += samples[i * 2 + 1] * g;
      }
    }

    return true;
  }
}

registerProcessor('mixer-worklet', MixerWorklet);
