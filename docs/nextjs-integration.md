# RescueStream API - Next.js Integration Guide

This guide covers integrating the RescueStream API into a Next.js application for managing live streams, broadcasters, and stream keys.

## Table of Contents

1. [Authentication Setup](#authentication-setup)
2. [API Client](#api-client)
3. [Server-Side Usage](#server-side-usage)
4. [React Hooks](#react-hooks)
5. [Stream Playback Components](#stream-playback-components)
6. [Example Pages](#example-pages)
7. [Error Handling](#error-handling)
8. [TypeScript Types](#typescript-types)

---

## Authentication Setup

The API uses HMAC-SHA256 signature-based authentication. All protected endpoints require three headers:

- `X-API-Key`: Your API key identifier
- `X-Signature`: HMAC-SHA256 signature of the request
- `X-Timestamp`: Unix timestamp (seconds)

### Environment Variables

```env
# .env.local
RESCUESTREAM_API_URL=https://api.rescuestream.example.com
RESCUESTREAM_API_KEY=your-api-key
RESCUESTREAM_API_SECRET=your-api-secret
```

---

## API Client

Create a server-side API client with automatic signature generation:

```typescript
// lib/rescuestream/client.ts
import crypto from 'crypto';

interface RequestOptions {
  method: 'GET' | 'POST' | 'PATCH' | 'DELETE';
  path: string;
  body?: unknown;
}

interface ApiError {
  type: string;
  title: string;
  status: number;
  detail: string;
  instance: string;
}

class RescueStreamClient {
  private baseUrl: string;
  private apiKey: string;
  private apiSecret: string;

  constructor() {
    this.baseUrl = process.env.RESCUESTREAM_API_URL!;
    this.apiKey = process.env.RESCUESTREAM_API_KEY!;
    this.apiSecret = process.env.RESCUESTREAM_API_SECRET!;

    if (!this.baseUrl || !this.apiKey || !this.apiSecret) {
      throw new Error('Missing RescueStream API configuration');
    }
  }

  private generateSignature(
    method: string,
    path: string,
    timestamp: number,
    body: string
  ): string {
    const stringToSign = `${method}\n${path}\n${timestamp}\n${body}`;
    return crypto
      .createHmac('sha256', this.apiSecret)
      .update(stringToSign)
      .digest('hex');
  }

  async request<T>({ method, path, body }: RequestOptions): Promise<T> {
    const timestamp = Math.floor(Date.now() / 1000);
    const bodyString = body ? JSON.stringify(body) : '';
    const signature = this.generateSignature(method, path, timestamp, bodyString);

    const response = await fetch(`${this.baseUrl}${path}`, {
      method,
      headers: {
        'Content-Type': 'application/json',
        'X-API-Key': this.apiKey,
        'X-Signature': signature,
        'X-Timestamp': timestamp.toString(),
      },
      body: bodyString || undefined,
    });

    if (!response.ok) {
      const error: ApiError = await response.json();
      throw new RescueStreamError(error);
    }

    // Handle 204 No Content
    if (response.status === 204) {
      return undefined as T;
    }

    return response.json();
  }

  // Broadcasters
  async listBroadcasters() {
    return this.request<BroadcastersResponse>({
      method: 'GET',
      path: '/broadcasters',
    });
  }

  async getBroadcaster(id: string) {
    return this.request<Broadcaster>({
      method: 'GET',
      path: `/broadcasters/${id}`,
    });
  }

  async createBroadcaster(data: CreateBroadcasterRequest) {
    return this.request<Broadcaster>({
      method: 'POST',
      path: '/broadcasters',
      body: data,
    });
  }

  async updateBroadcaster(id: string, data: UpdateBroadcasterRequest) {
    return this.request<Broadcaster>({
      method: 'PATCH',
      path: `/broadcasters/${id}`,
      body: data,
    });
  }

  async deleteBroadcaster(id: string) {
    return this.request<void>({
      method: 'DELETE',
      path: `/broadcasters/${id}`,
    });
  }

  // Stream Keys
  async listStreamKeys() {
    return this.request<StreamKeysResponse>({
      method: 'GET',
      path: '/stream-keys',
    });
  }

  async getStreamKey(id: string) {
    return this.request<StreamKey>({
      method: 'GET',
      path: `/stream-keys/${id}`,
    });
  }

  async createStreamKey(data: CreateStreamKeyRequest) {
    return this.request<StreamKey>({
      method: 'POST',
      path: '/stream-keys',
      body: data,
    });
  }

  async revokeStreamKey(id: string) {
    return this.request<void>({
      method: 'DELETE',
      path: `/stream-keys/${id}`,
    });
  }

  // Streams
  async listStreams() {
    return this.request<StreamsResponse>({
      method: 'GET',
      path: '/streams',
    });
  }

  async getStream(id: string) {
    return this.request<Stream>({
      method: 'GET',
      path: `/streams/${id}`,
    });
  }

  // Health
  async checkHealth() {
    const response = await fetch(`${this.baseUrl}/health`);
    return response.json() as Promise<HealthResponse>;
  }
}

// Singleton instance
let client: RescueStreamClient | null = null;

export function getRescueStreamClient(): RescueStreamClient {
  if (!client) {
    client = new RescueStreamClient();
  }
  return client;
}

export class RescueStreamError extends Error {
  type: string;
  status: number;
  detail: string;
  instance: string;

  constructor(error: ApiError) {
    super(error.title);
    this.name = 'RescueStreamError';
    this.type = error.type;
    this.status = error.status;
    this.detail = error.detail;
    this.instance = error.instance;
  }
}
```

---

## TypeScript Types

```typescript
// lib/rescuestream/types.ts

export interface Broadcaster {
  id: string;
  display_name: string;
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface CreateBroadcasterRequest {
  display_name: string;
  metadata?: Record<string, unknown>;
}

export interface UpdateBroadcasterRequest {
  display_name?: string;
  metadata?: Record<string, unknown>;
}

export interface BroadcastersResponse {
  broadcasters: Broadcaster[];
  count: number;
}

export interface StreamKey {
  id: string;
  key_value: string; // Only populated on creation
  broadcaster_id: string;
  status: 'active' | 'revoked' | 'expired';
  created_at: string;
  expires_at: string | null;
  revoked_at: string | null;
  last_used_at: string | null;
}

export interface CreateStreamKeyRequest {
  broadcaster_id: string;
  expires_at?: string; // RFC3339 format
}

export interface StreamKeysResponse {
  stream_keys: StreamKey[];
  count: number;
}

export interface StreamURLs {
  hls: string;
  webrtc: string;
}

export interface Stream {
  id: string;
  stream_key_id: string;
  path: string;
  status: 'active' | 'ended';
  started_at: string;
  ended_at: string | null;
  source_type: string | null;
  source_id: string | null;
  metadata: Record<string, unknown>;
  recording_ref: string | null;
  urls: StreamURLs;
}

export interface StreamsResponse {
  streams: Stream[];
  count: number;
}

export interface HealthResponse {
  status: 'ok' | 'degraded';
  database: 'ok' | 'unreachable';
}
```

---

## Server-Side Usage

### Server Actions (App Router)

```typescript
// app/actions/broadcasters.ts
'use server';

import { revalidatePath } from 'next/cache';
import { getRescueStreamClient, RescueStreamError } from '@/lib/rescuestream/client';
import type { CreateBroadcasterRequest, UpdateBroadcasterRequest } from '@/lib/rescuestream/types';

export async function createBroadcaster(data: CreateBroadcasterRequest) {
  try {
    const client = getRescueStreamClient();
    const broadcaster = await client.createBroadcaster(data);
    revalidatePath('/broadcasters');
    return { success: true, data: broadcaster };
  } catch (error) {
    if (error instanceof RescueStreamError) {
      return { success: false, error: error.detail };
    }
    throw error;
  }
}

export async function updateBroadcaster(id: string, data: UpdateBroadcasterRequest) {
  try {
    const client = getRescueStreamClient();
    const broadcaster = await client.updateBroadcaster(id, data);
    revalidatePath('/broadcasters');
    revalidatePath(`/broadcasters/${id}`);
    return { success: true, data: broadcaster };
  } catch (error) {
    if (error instanceof RescueStreamError) {
      return { success: false, error: error.detail };
    }
    throw error;
  }
}

export async function deleteBroadcaster(id: string) {
  try {
    const client = getRescueStreamClient();
    await client.deleteBroadcaster(id);
    revalidatePath('/broadcasters');
    return { success: true };
  } catch (error) {
    if (error instanceof RescueStreamError) {
      return { success: false, error: error.detail };
    }
    throw error;
  }
}
```

```typescript
// app/actions/stream-keys.ts
'use server';

import { revalidatePath } from 'next/cache';
import { getRescueStreamClient, RescueStreamError } from '@/lib/rescuestream/client';
import type { CreateStreamKeyRequest } from '@/lib/rescuestream/types';

export async function createStreamKey(data: CreateStreamKeyRequest) {
  try {
    const client = getRescueStreamClient();
    const streamKey = await client.createStreamKey(data);
    revalidatePath('/stream-keys');
    // Important: Return the key_value only on creation - it won't be available later
    return { success: true, data: streamKey };
  } catch (error) {
    if (error instanceof RescueStreamError) {
      return { success: false, error: error.detail };
    }
    throw error;
  }
}

export async function revokeStreamKey(id: string) {
  try {
    const client = getRescueStreamClient();
    await client.revokeStreamKey(id);
    revalidatePath('/stream-keys');
    revalidatePath('/streams'); // Active stream may have been terminated
    return { success: true };
  } catch (error) {
    if (error instanceof RescueStreamError) {
      return { success: false, error: error.detail };
    }
    throw error;
  }
}
```

### API Routes (Route Handlers)

```typescript
// app/api/streams/route.ts
import { NextResponse } from 'next/server';
import { getRescueStreamClient, RescueStreamError } from '@/lib/rescuestream/client';

export async function GET() {
  try {
    const client = getRescueStreamClient();
    const streams = await client.listStreams();
    return NextResponse.json(streams);
  } catch (error) {
    if (error instanceof RescueStreamError) {
      return NextResponse.json(
        { error: error.detail },
        { status: error.status }
      );
    }
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    );
  }
}
```

```typescript
// app/api/streams/[id]/route.ts
import { NextResponse } from 'next/server';
import { getRescueStreamClient, RescueStreamError } from '@/lib/rescuestream/client';

export async function GET(
  request: Request,
  { params }: { params: { id: string } }
) {
  try {
    const client = getRescueStreamClient();
    const stream = await client.getStream(params.id);
    return NextResponse.json(stream);
  } catch (error) {
    if (error instanceof RescueStreamError) {
      return NextResponse.json(
        { error: error.detail },
        { status: error.status }
      );
    }
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    );
  }
}
```

---

## React Hooks

Custom hooks for client-side data fetching with SWR:

```typescript
// hooks/use-streams.ts
'use client';

import useSWR from 'swr';
import type { Stream, StreamsResponse } from '@/lib/rescuestream/types';

const fetcher = (url: string) => fetch(url).then((res) => res.json());

export function useStreams() {
  const { data, error, isLoading, mutate } = useSWR<StreamsResponse>(
    '/api/streams',
    fetcher,
    {
      refreshInterval: 5000, // Poll every 5 seconds for live updates
    }
  );

  return {
    streams: data?.streams ?? [],
    count: data?.count ?? 0,
    isLoading,
    isError: !!error,
    refresh: mutate,
  };
}

export function useStream(id: string) {
  const { data, error, isLoading, mutate } = useSWR<Stream>(
    id ? `/api/streams/${id}` : null,
    fetcher,
    {
      refreshInterval: 5000,
    }
  );

  return {
    stream: data,
    isLoading,
    isError: !!error,
    refresh: mutate,
  };
}

export function useActiveStreams() {
  const { streams, ...rest } = useStreams();
  return {
    streams: streams.filter((s) => s.status === 'active'),
    ...rest,
  };
}
```

```typescript
// hooks/use-broadcasters.ts
'use client';

import useSWR from 'swr';
import type { Broadcaster, BroadcastersResponse } from '@/lib/rescuestream/types';

const fetcher = (url: string) => fetch(url).then((res) => res.json());

export function useBroadcasters() {
  const { data, error, isLoading, mutate } = useSWR<BroadcastersResponse>(
    '/api/broadcasters',
    fetcher
  );

  return {
    broadcasters: data?.broadcasters ?? [],
    count: data?.count ?? 0,
    isLoading,
    isError: !!error,
    refresh: mutate,
  };
}

export function useBroadcaster(id: string) {
  const { data, error, isLoading, mutate } = useSWR<Broadcaster>(
    id ? `/api/broadcasters/${id}` : null,
    fetcher
  );

  return {
    broadcaster: data,
    isLoading,
    isError: !!error,
    refresh: mutate,
  };
}
```

---

## Stream Playback Components

### HLS Video Player

```typescript
// components/hls-player.tsx
'use client';

import { useEffect, useRef } from 'react';
import Hls from 'hls.js';

interface HLSPlayerProps {
  src: string;
  autoPlay?: boolean;
  muted?: boolean;
  className?: string;
  onError?: (error: Error) => void;
  onPlaying?: () => void;
}

export function HLSPlayer({
  src,
  autoPlay = true,
  muted = true,
  className,
  onError,
  onPlaying,
}: HLSPlayerProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !src) return;

    if (Hls.isSupported()) {
      const hls = new Hls({
        enableWorker: true,
        lowLatencyMode: true,
        backBufferLength: 90,
      });

      hlsRef.current = hls;

      hls.loadSource(src);
      hls.attachMedia(video);

      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        if (autoPlay) {
          video.play().catch(() => {
            // Autoplay blocked, user interaction required
          });
        }
      });

      hls.on(Hls.Events.ERROR, (_, data) => {
        if (data.fatal) {
          switch (data.type) {
            case Hls.ErrorTypes.NETWORK_ERROR:
              hls.startLoad();
              break;
            case Hls.ErrorTypes.MEDIA_ERROR:
              hls.recoverMediaError();
              break;
            default:
              onError?.(new Error(data.details));
              break;
          }
        }
      });

      return () => {
        hls.destroy();
        hlsRef.current = null;
      };
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      // Safari native HLS support
      video.src = src;
      if (autoPlay) {
        video.play().catch(() => {});
      }
    }
  }, [src, autoPlay, onError]);

  return (
    <video
      ref={videoRef}
      className={className}
      autoPlay={autoPlay}
      muted={muted}
      playsInline
      controls
      onPlaying={onPlaying}
    />
  );
}
```

### WebRTC Player (WHEP)

```typescript
// components/webrtc-player.tsx
'use client';

import { useEffect, useRef, useState } from 'react';

interface WebRTCPlayerProps {
  src: string; // WHEP endpoint URL
  className?: string;
  onError?: (error: Error) => void;
  onPlaying?: () => void;
}

export function WebRTCPlayer({
  src,
  className,
  onError,
  onPlaying,
}: WebRTCPlayerProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const pcRef = useRef<RTCPeerConnection | null>(null);
  const [isConnecting, setIsConnecting] = useState(true);

  useEffect(() => {
    if (!src) return;

    const connect = async () => {
      try {
        setIsConnecting(true);

        const pc = new RTCPeerConnection({
          iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
        });
        pcRef.current = pc;

        pc.addTransceiver('video', { direction: 'recvonly' });
        pc.addTransceiver('audio', { direction: 'recvonly' });

        pc.ontrack = (event) => {
          if (videoRef.current && event.streams[0]) {
            videoRef.current.srcObject = event.streams[0];
          }
        };

        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);

        // Wait for ICE gathering
        await new Promise<void>((resolve) => {
          if (pc.iceGatheringState === 'complete') {
            resolve();
          } else {
            pc.onicegatheringstatechange = () => {
              if (pc.iceGatheringState === 'complete') {
                resolve();
              }
            };
          }
        });

        // WHEP request
        const response = await fetch(src, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/sdp',
          },
          body: pc.localDescription?.sdp,
        });

        if (!response.ok) {
          throw new Error(`WHEP request failed: ${response.status}`);
        }

        const answerSdp = await response.text();
        await pc.setRemoteDescription({
          type: 'answer',
          sdp: answerSdp,
        });

        setIsConnecting(false);
      } catch (error) {
        setIsConnecting(false);
        onError?.(error instanceof Error ? error : new Error(String(error)));
      }
    };

    connect();

    return () => {
      pcRef.current?.close();
      pcRef.current = null;
    };
  }, [src, onError]);

  return (
    <div className="relative">
      {isConnecting && (
        <div className="absolute inset-0 flex items-center justify-center bg-black/50">
          <span className="text-white">Connecting...</span>
        </div>
      )}
      <video
        ref={videoRef}
        className={className}
        autoPlay
        muted
        playsInline
        controls
        onPlaying={onPlaying}
      />
    </div>
  );
}
```

### Stream Player with Protocol Selection

```typescript
// components/stream-player.tsx
'use client';

import { useState } from 'react';
import { HLSPlayer } from './hls-player';
import { WebRTCPlayer } from './webrtc-player';
import type { StreamURLs } from '@/lib/rescuestream/types';

interface StreamPlayerProps {
  urls: StreamURLs;
  className?: string;
  defaultProtocol?: 'hls' | 'webrtc';
}

export function StreamPlayer({
  urls,
  className,
  defaultProtocol = 'hls',
}: StreamPlayerProps) {
  const [protocol, setProtocol] = useState<'hls' | 'webrtc'>(defaultProtocol);
  const [error, setError] = useState<string | null>(null);

  const handleError = (err: Error) => {
    setError(err.message);
    // Fallback to alternative protocol
    if (protocol === 'webrtc' && urls.hls) {
      setProtocol('hls');
      setError(null);
    }
  };

  return (
    <div className="space-y-2">
      <div className="flex gap-2">
        <button
          onClick={() => { setProtocol('hls'); setError(null); }}
          className={`px-3 py-1 rounded ${
            protocol === 'hls' ? 'bg-blue-600 text-white' : 'bg-gray-200'
          }`}
        >
          HLS
        </button>
        <button
          onClick={() => { setProtocol('webrtc'); setError(null); }}
          className={`px-3 py-1 rounded ${
            protocol === 'webrtc' ? 'bg-blue-600 text-white' : 'bg-gray-200'
          }`}
        >
          WebRTC (Low Latency)
        </button>
      </div>

      {error && (
        <div className="text-red-500 text-sm">{error}</div>
      )}

      {protocol === 'hls' ? (
        <HLSPlayer src={urls.hls} className={className} onError={handleError} />
      ) : (
        <WebRTCPlayer src={urls.webrtc} className={className} onError={handleError} />
      )}
    </div>
  );
}
```

---

## Example Pages

### Streams List Page

```typescript
// app/streams/page.tsx
import { getRescueStreamClient } from '@/lib/rescuestream/client';
import { StreamsList } from './streams-list';

export const revalidate = 10; // Revalidate every 10 seconds

export default async function StreamsPage() {
  const client = getRescueStreamClient();
  const { streams, count } = await client.listStreams();

  return (
    <div className="container mx-auto py-8">
      <h1 className="text-2xl font-bold mb-6">Live Streams ({count})</h1>
      <StreamsList initialStreams={streams} />
    </div>
  );
}
```

```typescript
// app/streams/streams-list.tsx
'use client';

import Link from 'next/link';
import { useActiveStreams } from '@/hooks/use-streams';
import type { Stream } from '@/lib/rescuestream/types';

interface StreamsListProps {
  initialStreams: Stream[];
}

export function StreamsList({ initialStreams }: StreamsListProps) {
  const { streams, isLoading } = useActiveStreams();
  const displayStreams = streams.length > 0 ? streams : initialStreams;

  if (displayStreams.length === 0) {
    return <p className="text-gray-500">No active streams</p>;
  }

  return (
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
      {displayStreams.map((stream) => (
        <Link
          key={stream.id}
          href={`/streams/${stream.id}`}
          className="block p-4 border rounded-lg hover:border-blue-500"
        >
          <div className="flex items-center gap-2 mb-2">
            <span className="w-2 h-2 bg-red-500 rounded-full animate-pulse" />
            <span className="font-medium">Live</span>
          </div>
          <p className="text-sm text-gray-600">
            Started: {new Date(stream.started_at).toLocaleString()}
          </p>
        </Link>
      ))}
    </div>
  );
}
```

### Stream Watch Page

```typescript
// app/streams/[id]/page.tsx
import { notFound } from 'next/navigation';
import { getRescueStreamClient, RescueStreamError } from '@/lib/rescuestream/client';
import { StreamPlayer } from '@/components/stream-player';

interface StreamPageProps {
  params: { id: string };
}

export default async function StreamPage({ params }: StreamPageProps) {
  const client = getRescueStreamClient();

  try {
    const stream = await client.getStream(params.id);

    if (stream.status !== 'active') {
      return (
        <div className="container mx-auto py-8">
          <h1 className="text-2xl font-bold mb-4">Stream Ended</h1>
          <p>This stream ended at {new Date(stream.ended_at!).toLocaleString()}</p>
        </div>
      );
    }

    return (
      <div className="container mx-auto py-8">
        <h1 className="text-2xl font-bold mb-6">Live Stream</h1>
        <StreamPlayer
          urls={stream.urls}
          className="w-full max-w-4xl aspect-video"
        />
        <div className="mt-4 text-sm text-gray-600">
          <p>Started: {new Date(stream.started_at).toLocaleString()}</p>
          <p>Source: {stream.source_type ?? 'Unknown'}</p>
        </div>
      </div>
    );
  } catch (error) {
    if (error instanceof RescueStreamError && error.status === 404) {
      notFound();
    }
    throw error;
  }
}
```

### Broadcaster Management Page

```typescript
// app/broadcasters/page.tsx
import { getRescueStreamClient } from '@/lib/rescuestream/client';
import { BroadcasterForm } from './broadcaster-form';
import { BroadcasterList } from './broadcaster-list';

export default async function BroadcastersPage() {
  const client = getRescueStreamClient();
  const { broadcasters } = await client.listBroadcasters();

  return (
    <div className="container mx-auto py-8">
      <h1 className="text-2xl font-bold mb-6">Broadcasters</h1>

      <div className="mb-8">
        <h2 className="text-lg font-semibold mb-4">Create New Broadcaster</h2>
        <BroadcasterForm />
      </div>

      <BroadcasterList broadcasters={broadcasters} />
    </div>
  );
}
```

```typescript
// app/broadcasters/broadcaster-form.tsx
'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { createBroadcaster } from '@/app/actions/broadcasters';

export function BroadcasterForm() {
  const router = useRouter();
  const [displayName, setDisplayName] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSubmitting(true);
    setError(null);

    const result = await createBroadcaster({ display_name: displayName });

    if (result.success) {
      setDisplayName('');
      router.refresh();
    } else {
      setError(result.error ?? 'Failed to create broadcaster');
    }

    setIsSubmitting(false);
  };

  return (
    <form onSubmit={handleSubmit} className="flex gap-4">
      <input
        type="text"
        value={displayName}
        onChange={(e) => setDisplayName(e.target.value)}
        placeholder="Display Name"
        required
        className="flex-1 px-4 py-2 border rounded"
      />
      <button
        type="submit"
        disabled={isSubmitting}
        className="px-6 py-2 bg-blue-600 text-white rounded disabled:opacity-50"
      >
        {isSubmitting ? 'Creating...' : 'Create'}
      </button>
      {error && <span className="text-red-500">{error}</span>}
    </form>
  );
}
```

---

## Error Handling

### Error Boundary Component

```typescript
// components/error-boundary.tsx
'use client';

import { useEffect } from 'react';

interface ErrorBoundaryProps {
  error: Error & { digest?: string };
  reset: () => void;
}

export default function ErrorBoundary({ error, reset }: ErrorBoundaryProps) {
  useEffect(() => {
    console.error('Stream error:', error);
  }, [error]);

  return (
    <div className="p-4 border border-red-200 bg-red-50 rounded">
      <h2 className="text-lg font-semibold text-red-800">Something went wrong</h2>
      <p className="text-red-600 mt-2">{error.message}</p>
      <button
        onClick={reset}
        className="mt-4 px-4 py-2 bg-red-600 text-white rounded hover:bg-red-700"
      >
        Try again
      </button>
    </div>
  );
}
```

### API Error Handler Utility

```typescript
// lib/rescuestream/errors.ts
import { RescueStreamError } from './client';

export function getErrorMessage(error: unknown): string {
  if (error instanceof RescueStreamError) {
    switch (error.type) {
      case '/errors/not-found':
        return 'The requested resource was not found.';
      case '/errors/unauthorized':
        return 'Authentication failed. Please check your API credentials.';
      case '/errors/conflict':
        return error.detail;
      case '/errors/stream-key-in-use':
        return 'This stream key is already being used.';
      case '/errors/stream-key-revoked':
        return 'This stream key has been revoked.';
      case '/errors/stream-key-expired':
        return 'This stream key has expired.';
      default:
        return error.detail || 'An unexpected error occurred.';
    }
  }

  if (error instanceof Error) {
    return error.message;
  }

  return 'An unexpected error occurred.';
}
```

---

## Installation

```bash
# Install required dependencies
npm install swr hls.js

# TypeScript types for hls.js
npm install -D @types/hls.js
```

## Security Notes

1. **Never expose API credentials client-side** - All authenticated API calls must go through your Next.js server (API routes or server actions)

2. **Stream key handling** - The `key_value` is only returned when creating a new stream key. Store it securely and display it to users only once

3. **Signature timing** - The API rejects requests with timestamps more than 5 minutes old, preventing replay attacks

4. **Environment variables** - Use `.env.local` for local development and proper secrets management in production

---

## API Reference Quick Guide

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Check API health |
| GET | `/streams` | List all streams |
| GET | `/streams/{id}` | Get stream by ID |
| GET | `/stream-keys` | List stream keys |
| POST | `/stream-keys` | Create stream key |
| GET | `/stream-keys/{id}` | Get stream key |
| DELETE | `/stream-keys/{id}` | Revoke stream key |
| GET | `/broadcasters` | List broadcasters |
| POST | `/broadcasters` | Create broadcaster |
| GET | `/broadcasters/{id}` | Get broadcaster |
| PATCH | `/broadcasters/{id}` | Update broadcaster |
| DELETE | `/broadcasters/{id}` | Delete broadcaster |
