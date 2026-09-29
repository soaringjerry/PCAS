FROM python:3.12-slim-bookworm
RUN pip install --no-cache-dir fastembed==0.8.1 \
    && groupadd -g 10001 pcas && useradd -u 10001 -g pcas -m pcas \
    && mkdir /models && chown pcas:pcas /models
COPY server.py /app/server.py
USER pcas
ENV PCAS_EMBEDDING_MODEL=BAAI/bge-small-zh-v1.5 PCAS_EMBEDDING_THREADS=2
EXPOSE 8080
CMD ["python", "/app/server.py"]
