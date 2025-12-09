import http from 'k6/http';
import { sleep } from 'k6';

export const options = {
  scenarios: {
    default: {
      executor: 'shared-iterations',
      vus: 10,
      iterations: 1000,
      maxDuration: '30s',
    },
  },
};

export default function () {
  const url = 'http://localhost:8080/api/event';

  const payload = JSON.stringify({
    'type': (Math.random() < 0.005 ? 'Emergency' : 'Position'),
    'vehicle_plate': 'ABC-123',
    'coordinates': {
      'latitude': 12.345,
      'longitude': 67.890,
    },
    'status': 'OK',
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
    },
  };

  const res = http.post(url, payload, params);

  sleep(0.25);
}