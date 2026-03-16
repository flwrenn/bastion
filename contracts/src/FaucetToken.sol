// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.28;

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";

/// @title FaucetToken
/// @notice Demo ERC-20 token with a public faucet. Anyone can call `claim()`
///         to mint a fixed amount to themselves, subject to a cooldown.
///         Used for testing SmartAccount interactions with ERC-20 transfers.
contract FaucetToken is ERC20 {
    /// @notice Amount minted per claim (100 tokens with 18 decimals).
    uint256 public constant CLAIM_AMOUNT = 100 ether;

    /// @notice Minimum time between claims for the same address.
    uint256 public constant CLAIM_COOLDOWN = 1 hours;

    /// @notice Tracks the last claim timestamp for each address.
    mapping(address => uint256) public lastClaimed;

    /// @notice Thrown when a caller tries to claim before the cooldown expires.
    error ClaimCooldown(uint256 availableAt);

    constructor() ERC20("Bastion Faucet Token", "BFT") {}

    /// @notice Mint `CLAIM_AMOUNT` tokens to the caller. Reverts if the
    ///         cooldown has not elapsed since the caller's last claim.
    function claim() external {
        uint256 availableAt = lastClaimed[msg.sender] + CLAIM_COOLDOWN;
        if (block.timestamp < availableAt) {
            revert ClaimCooldown(availableAt);
        }

        lastClaimed[msg.sender] = block.timestamp;
        _mint(msg.sender, CLAIM_AMOUNT);
    }
}
