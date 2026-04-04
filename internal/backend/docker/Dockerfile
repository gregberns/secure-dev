FROM ubuntu:24.04

RUN apt-get update && apt-get install -y \
    openssh-server sudo bash curl git \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash ubuntu \
    && echo "ubuntu ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers \
    && mkdir -p /home/ubuntu/.ssh \
    && chmod 700 /home/ubuntu/.ssh \
    && chown ubuntu:ubuntu /home/ubuntu/.ssh

RUN mkdir /run/sshd \
    && sed -i 's/#AcceptEnv/AcceptEnv/' /etc/ssh/sshd_config \
    && echo "AcceptEnv SD_* GITHUB_* GH_* ANTHROPIC_*" >> /etc/ssh/sshd_config

EXPOSE 22
CMD ["/usr/sbin/sshd", "-D"]
