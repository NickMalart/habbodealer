#!/usr/bin/env python3
import argparse
import json
import re
import sys
from typing import List, Dict, Optional, Tuple

FIGURE_RE = re.compile(r'^(hr|hd|ch|lg|sh|ea|ha|fa|ca|wa)-\d+-\d+')
MOTTO_SKIP_RE = re.compile(r'^[A-Z]{3,4}\d')


def read_packet_bytes(input_path: Optional[str]) -> bytes:
    if input_path:
        with open(input_path, 'rb') as f:
            return f.read()
    return sys.stdin.buffer.read()


def split_packet_fields(packet: bytes) -> List[Dict[str, object]]:
    """Return list of field dicts with raw bytes and ascii-decoded string.

    Keeps the original raw bytes (for token extraction) while preserving
    the previous ascii/ignore decoding used by the parser heuristics.
    """
    parts: List[Dict[str, object]] = []
    for part in packet.split(b'\x02'):
        parts.append({
            'raw': part,
            'str': part.decode('ascii', errors='ignore').strip(),
        })
    return parts


def vl64_chunk_length(text: str) -> int:
    if not text:
        return 0
    length = (ord(text[0]) >> 3) & 7
    if length <= 0:
        length = 1
    return length


def decode_vl64(chunk: str) -> Optional[int]:
    if not chunk:
        return None
    try:
        vals = [ord(c) for c in chunk]
        first = vals[0]
        total_bytes = (first >> 3) & 7
        if total_bytes <= 0:
            total_bytes = 1
        negative = (first & 4) != 0
        value = first & 3
        shift = 2
        for b in vals[1:total_bytes]:
            value |= (b & 0x3F) << shift
            shift += 6
        if negative:
            value = -value
        return value
    except Exception:
        return None


def read_fixed_vl64_prefix(text: str, count: int) -> Tuple[List[str], str]:
    parts: List[str] = []
    remaining = text
    for _ in range(count):
        if not remaining:
            break
        length = vl64_chunk_length(remaining)
        current = remaining[:length]
        parts.append(current)
        remaining = remaining[length:]
    return parts, remaining


def prefix_count_from_live_block(text: str) -> int:
    if not text:
        return 3
    first_len = vl64_chunk_length(text)
    if first_len == 2:
        return 5
    return 3


def extract_entity_and_username(name_block: str, name_block_raw: bytes) -> Dict[str, object]:
    """Parse the name_block string and include raw bytes token hex.

    `name_block` is the ascii-decoded form used by the existing heuristics.
    `name_block_raw` is the raw bytes for the same field (may contain non-ascii
    token bytes). We emit `token_hex` computed from the first up-to-4 bytes
    of the raw block so external code can reliably detect encoded tokens.
    """
    original = name_block
    working = name_block
    if working.startswith('@\\'):
        working = working[2:]

    num_ints = prefix_count_from_live_block(working)
    parsed_ints, remaining = read_fixed_vl64_prefix(working, num_ints)

    # If the prefix parser consumed all remaining text, the last parsed
    # chunk is likely the username rather than an integer prefix.
    if not remaining and parsed_ints:
        remaining = parsed_ints.pop()

    # If the remaining username portion is very short (e.g. "on"), it's
    # likely the true username was split across the final prefix chunks.
    # Merge any trailing alphabetic chunks from parsed_ints into `remaining`.
    if remaining is not None and len(remaining) <= 2 and parsed_ints:
        while parsed_ints and parsed_ints[-1].isalpha():
            candidate = parsed_ints[-1]
            # Avoid swallowing chunks that look like lowercase+uppercase
            # suffixes (e.g. "cucM") which are likely prefix fragments.
            if re.match(r'^[a-z]+[A-Z]$', candidate):
                break
            remaining = parsed_ints.pop() + remaining

    # Heuristic: sometimes the last parsed chunk is a short alphabetic
    # fragment that belongs to the username (e.g. "D" + "rHabloon"). If
    # so, prepend it to the username and drop it from parsed prefixes.
    if parsed_ints and remaining:
        last_chunk = parsed_ints[-1]
        if len(last_chunk) <= 2 and last_chunk.isalpha() and remaining[0].isalpha():
            remaining = parsed_ints.pop() + remaining

    chat_id_raw = parsed_ints[-2] if len(parsed_ints) >= 2 else ''
    trade_id_raw = parsed_ints[-1] if len(parsed_ints) >= 1 else ''
    entity_id = ''.join(parsed_ints)

    # Compute token_hex from the first up-to-4 raw bytes of the name block.
    token_hex = ''
    try:
        if name_block_raw:
            token_hex = name_block_raw[:4].hex()
    except Exception:
        token_hex = ''

    return {
        'entity_id': entity_id,
        'chat_id_raw': chat_id_raw,
        'trade_id_raw': trade_id_raw,
        'chat_id': decode_vl64(chat_id_raw),
        'trade_id': decode_vl64(trade_id_raw),
        'username': remaining,
        'raw_name_block': original,
        'token_hex': token_hex,
    }


def parse_users28(packet: bytes) -> List[Dict[str, object]]:
    fields = split_packet_fields(packet)
    users: List[Dict[str, object]] = []

    for i, field in enumerate(fields):
        field_str = field['str']
        if not FIGURE_RE.match(field_str):
            continue
        # Allow index 0 (there may be no preceding field); use safe indexing.
        name_block_str = fields[i - 1]['str'] if i > 0 else ''
        name_block_raw = fields[i - 1]['raw'] if i > 0 else b''
        parsed = extract_entity_and_username(name_block_str, name_block_raw)
        sex = fields[i + 1]['str'] if i + 1 < len(fields) else ''

        motto = ''
        if i + 2 < len(fields):
            candidate = fields[i + 2]['str']
            if (
                len(candidate) > 5
                and not MOTTO_SKIP_RE.match(candidate)
                and candidate not in {'Istd', 'std'}
            ):
                motto = candidate

        users.append({
            'username': parsed['username'],
            'trade_id': parsed['trade_id'],
            'trade_id_raw': parsed['trade_id_raw'],
            'chat_id': parsed['chat_id'],
            'chat_id_raw': parsed['chat_id_raw'],
            'entity_id': parsed['entity_id'],
            'figure': field_str,
            'sex': sex,
            'motto': motto,
            'token_hex': parsed.get('token_hex', ''),
        })

    users.sort(key=lambda u: (
        u.get('chat_id') if isinstance(u.get('chat_id'), int) and u.get('chat_id') is not None else -1,
        u.get('trade_id') if isinstance(u.get('trade_id'), int) and u.get('trade_id') is not None else -1,
        str(u.get('username', '')),
    ))
    return users


def main() -> int:
    parser = argparse.ArgumentParser(description='Parse raw USERS[28] packet bytes into JSON.')
    parser.add_argument('--input', help='Path to a file containing the exact raw packet bytes.')
    parser.add_argument('--json', action='store_true', help='Emit JSON only.')
    args = parser.parse_args()

    packet = read_packet_bytes(args.input)
    users = parse_users28(packet)

    if args.json:
        sys.stdout.write(json.dumps(users, ensure_ascii=False, separators=(',', ':')))
        return 0

    for user in users:
        sys.stdout.write(json.dumps(user, ensure_ascii=False) + '\n')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
